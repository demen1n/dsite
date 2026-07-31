package handlers

import (
	"dsite/internal/db"
	"dsite/internal/imgproc"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Уменьшение и перекодирование загруженных изображений на сервере: единая
// точка правды для качества/резкости независимо от того, какой браузер
// и с каким canvas-движком отправил файл.
const (
	uploadMaxWidth = 1600
	uploadQuality  = 85
	// coverMaxHeight caps only "cover" images (post/series cover, avatar) —
	// they're always displayed cropped into a bounded box (object-fit:cover
	// with a max-height), so a portrait photo doesn't need its full, much
	// taller-than-shown long edge stored. Gallery photos and inline post
	// images are shown at full aspect ratio and stay width-only capped.
	coverMaxHeight = 1000
)

var dummyHash []byte

func init() {
	dummyHash, _ = bcrypt.GenerateFromPassword([]byte("_"), bcrypt.DefaultCost)
}

// ─────────────────────── Auth ───────────────────────

// GET /admin/setup — первичная настройка, если нет юзеров
func Setup(w http.ResponseWriter, r *http.Request) {
	exists, _ := db.UserExists()
	if exists {
		http.Redirect(w, r, "/admin", http.StatusFound)
		return
	}
	if r.Method == http.MethodPost {
		login := r.FormValue("login")
		pass := r.FormValue("password")
		if login == "" || pass == "" {
			renderFragment(w, "admin/login.html", "admin/setup.html", page("Настройка", "Заполните все поля"))
			return
		}
		if len(pass) < 8 {
			renderFragment(w, "admin/login.html", "admin/setup.html", page("Настройка", "Пароль должен быть не короче 8 символов"))
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
		if err != nil {
			http.Error(w, "hash error", 500)
			return
		}
		if err := db.CreateUser(login, string(hash)); err != nil {
			log.Printf("create user: %v", err)
			renderFragment(w, "admin/login.html", "admin/setup.html", page("Настройка", "Ошибка при создании пользователя"))
			return
		}
		http.Redirect(w, r, "/admin/login", http.StatusFound)
		return
	}
	renderFragment(w, "admin/login.html", "admin/setup.html", page("Настройка", ""))
}

// GET/POST /admin/login
func Login(w http.ResponseWriter, r *http.Request) {
	exists, _ := db.UserExists()
	if !exists {
		http.Redirect(w, r, "/admin/setup", http.StatusFound)
		return
	}
	if r.Method == http.MethodPost {
		if !LoginAllowed(r) {
			renderFragment(w, "admin/login.html", "login.html", page("Вход", "Слишком много попыток. Повторите через 15 минут."))
			return
		}
		login := r.FormValue("login")
		pass := r.FormValue("password")
		_, hash, err := db.GetUserByLogin(login)
		if err != nil {
			hash = string(dummyHash)
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(pass)) != nil || err != nil {
			RecordLoginFailure(r)
			renderFragment(w, "admin/login.html", "login.html", page("Вход", "Неверный логин или пароль"))
			return
		}
		RecordLoginSuccess(r)
		token := NewSession()
		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			Secure:   secureCookies,
			SameSite: http.SameSiteLaxMode,
			Expires:  time.Now().Add(30 * 24 * time.Hour),
		})
		http.Redirect(w, r, "/admin", http.StatusFound)
		return
	}
	renderFragment(w, "admin/login.html", "login.html", page("Вход", ""))
}

// POST /admin/logout
func Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("session"); err == nil {
		DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "session", MaxAge: -1, Path: "/", HttpOnly: true, Secure: secureCookies, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/", http.StatusFound)
}

// ─────────────────────── Admin dashboard ───────────────────────

// GET /admin
func AdminIndex(w http.ResponseWriter, r *http.Request) {
	posts, err := db.ListPosts(false)
	if err != nil {
		http.Error(w, "DB error", 500)
		return
	}
	render(w, "admin/index.html", page("Панель управления", posts))
}

// ─────────────────────── Posts CRUD ───────────────────────

// GET /admin/posts/new
func NewPost(w http.ResponseWriter, r *http.Request) {
	pd := page("Новый пост", nil)
	pd.AllTags = loadAllTagNames()
	pd.AllSeries, _ = db.ListSeries()
	render(w, "admin/editor.html", pd)
}

// POST /admin/posts/new
func CreatePost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	defer r.MultipartForm.RemoveAll()

	title := r.FormValue("title")
	bodyMD := r.FormValue("body")
	published := r.FormValue("published") == "1"

	slug := r.FormValue("slug")
	if slug == "" {
		slug = slugify(title)
	}
	if slug == "" {
		slug = fmt.Sprintf("post-%d", time.Now().Unix())
	}

	bodyHTML, err := RenderMD(bodyMD)
	if err != nil {
		http.Error(w, "markdown error", 500)
		return
	}

	cover, err := handleCoverUpload(r)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	seriesID, _ := strconv.Atoi(r.FormValue("series_id"))
	id, err := db.CreatePost(slug, title, bodyMD, bodyHTML, cover, published, seriesID)
	if err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}

	if tagsStr := r.FormValue("tags"); tagsStr != "" {
		db.SetPostTags(int(id), parseTags(tagsStr))
	}

	http.Redirect(w, r, fmt.Sprintf("/admin/posts/%d/edit", id), http.StatusFound)
}

// GET /admin/posts/{id}/edit
func EditPostForm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	post, err := db.GetPostByID(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	pd := page("Редактировать пост", post)
	pd.AllTags = loadAllTagNames()
	pd.AllSeries, _ = db.ListSeries()
	render(w, "admin/editor.html", pd)
}

// POST /admin/posts/{id}/edit
func UpdatePost(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	defer r.MultipartForm.RemoveAll()

	title := r.FormValue("title")
	bodyMD := r.FormValue("body")
	slug := r.FormValue("slug")
	published := r.FormValue("published") == "1"

	bodyHTML, err := RenderMD(bodyMD)
	if err != nil {
		http.Error(w, "markdown error", 500)
		return
	}

	// Обложка — только если загружена новая
	cover, err := handleCoverUpload(r)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if cover == "" && r.FormValue("remove_cover") != "1" {
		// Оставляем старую
		existing, _ := db.GetPostByID(id)
		if existing != nil {
			cover = existing.Cover
		}
	}

	seriesID, _ := strconv.Atoi(r.FormValue("series_id"))
	if err := db.UpdatePost(id, slug, title, bodyMD, bodyHTML, cover, published, seriesID); err != nil {
		http.Error(w, "DB error", 500)
		return
	}
	db.SetPostTags(id, parseTags(r.FormValue("tags")))
	http.Redirect(w, r, fmt.Sprintf("/admin/posts/%d/edit", id), http.StatusFound)
}

// POST /admin/posts/{id}/delete
func DeletePost(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	db.DeletePost(id)
	http.Redirect(w, r, "/admin", http.StatusFound)
}

// POST /admin/posts/preview  — возвращает HTML из Markdown (HTMX)
func PreviewMD(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	body := r.FormValue("body")
	html, err := RenderMD(body)
	if err != nil {
		http.Error(w, "markdown error", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// ─────────────────────── Series ───────────────────────

// GET /admin/series
func AdminSeries(w http.ResponseWriter, r *http.Request) {
	list, err := db.ListSeries()
	if err != nil {
		http.Error(w, "DB error", 500)
		return
	}
	render(w, "admin/series.html", page("Серии", list))
}

// GET /admin/series/new
func NewSeriesForm(w http.ResponseWriter, r *http.Request) {
	render(w, "admin/series_form.html", page("Новая серия", nil))
}

// POST /admin/series/new
func CreateSeries(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	defer r.MultipartForm.RemoveAll()

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name required", 400)
		return
	}
	slug := strings.TrimSpace(r.FormValue("slug"))
	if slug == "" {
		slug = slugify(name)
	}
	if slug == "" {
		http.Error(w, "invalid name", 400)
		return
	}
	descMD := r.FormValue("description")
	descHTML, err := RenderMD(descMD)
	if err != nil {
		http.Error(w, "markdown error", 500)
		return
	}
	cover, err := handleCoverUpload(r)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	collectPhotos := r.FormValue("collect_photos") == "1"
	showCover := r.FormValue("show_cover") == "1"
	id, err := db.CreateSeries(name, slug, descMD, descHTML, cover, collectPhotos, showCover)
	if err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/admin/series/%d/edit", id), http.StatusFound)
}

// GET /admin/series/{id}/edit
func EditSeriesForm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	series, err := db.GetSeriesByID(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	render(w, "admin/series_form.html", page("Редактировать серию", series))
}

// POST /admin/series/{id}/edit
func UpdateSeries(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	defer r.MultipartForm.RemoveAll()

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name required", 400)
		return
	}
	slug := strings.TrimSpace(r.FormValue("slug"))
	if slug == "" {
		slug = slugify(name)
	}
	if slug == "" {
		http.Error(w, "invalid name", 400)
		return
	}
	descMD := r.FormValue("description")
	descHTML, err := RenderMD(descMD)
	if err != nil {
		http.Error(w, "markdown error", 500)
		return
	}
	cover, err := handleCoverUpload(r)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if cover == "" && r.FormValue("remove_cover") != "1" {
		existing, _ := db.GetSeriesByID(id)
		if existing != nil {
			cover = existing.Cover
		}
	}
	collectPhotos := r.FormValue("collect_photos") == "1"
	showCover := r.FormValue("show_cover") == "1"
	if err := db.UpdateSeries(id, name, slug, descMD, descHTML, cover, collectPhotos, showCover); err != nil {
		http.Error(w, "DB error", 500)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/admin/series/%d/edit", id), http.StatusFound)
}

// POST /admin/series/{id}/delete
func DeleteSeries(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	db.DeleteSeries(id)
	http.Redirect(w, r, "/admin/series", http.StatusFound)
}

// ─────────────────────── Media ───────────────────────

// GET /admin/media/picker — фрагмент с галереей для вставки в пост
func MediaPicker(w http.ResponseWriter, r *http.Request) {
	photos, _ := db.ListPhotos("", "")
	renderFragment(w, "admin/media_picker.html", "media_picker", page("Медиа", GalleryAdminData{Photos: photos}))
}

// POST /admin/media/to-gallery — добавить существующий файл в галерею
func AddToGallery(w http.ResponseWriter, r *http.Request) {
	filename := filepath.Base(r.FormValue("filename"))
	if filename == "" || filename == "." {
		http.Error(w, "invalid filename", 400)
		return
	}
	// Проверяем что файл реально существует в uploads
	if _, err := os.Stat(filepath.Join(uploadsDir, filename)); err != nil {
		http.Error(w, "file not found", 404)
		return
	}
	caption := r.FormValue("caption")
	if err := db.AddPhoto(filename, caption, 0, 0, 0, 0); err != nil {
		http.Error(w, "DB error", 500)
		return
	}
	fmt.Fprint(w, "✓ Добавлено в галерею")
}

// ─────────────────────── Gallery ───────────────────────

type GalleryAdminData struct {
	Photos     []db.Photo
	Categories []db.Category
	Places     []db.Place
}

// GET /admin/gallery
func AdminGallery(w http.ResponseWriter, r *http.Request) {
	photos, err := db.ListPhotos("", "")
	if err != nil {
		http.Error(w, "DB error", 500)
		return
	}
	cats, err := db.ListCategories()
	if err != nil {
		http.Error(w, "DB error", 500)
		return
	}
	places, err := db.ListPlaces()
	if err != nil {
		http.Error(w, "DB error", 500)
		return
	}
	render(w, "admin/gallery.html", page("Галерея", GalleryAdminData{Photos: photos, Categories: cats, Places: places}))
}

// POST /admin/gallery/upload
func UploadPhoto(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 30<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	defer r.MultipartForm.RemoveAll()

	files := r.MultipartForm.File["photos"]
	caption := r.FormValue("caption")
	categoryID, _ := strconv.Atoi(r.FormValue("category_id"))
	placeID, _ := strconv.Atoi(r.FormValue("place_id"))

	for _, fh := range files {
		// Each file's read+process+save runs under a single reserved slot, so
		// concurrent upload requests can't each hold their full source buffer
		// in RAM while queued (see imgproc.Reserve). A closure keeps the
		// release scoped to one file instead of the whole request.
		func() {
			release := imgproc.Reserve()
			defer release()

			f, err := fh.Open()
			if err != nil {
				return
			}
			data, err := io.ReadAll(f)
			f.Close()
			if err != nil {
				return
			}

			ext := strings.ToLower(filepath.Ext(fh.Filename))
			if ext == "" {
				ext = ".webp"
			}
			if !isAllowedImageExt(ext) || !isAllowedImageContent(data) {
				return
			}

			var filename string
			var pw, ph int
			if ext == ".gif" { // анимация — сохраняем как есть, без перекодирования
				filename, err = saveUpload(data, ext)
				if err == nil {
					pw, ph, _ = imgproc.Dimensions(data)
				}
			} else {
				var processed []byte
				processed, pw, ph, err = imgproc.Process(data, uploadMaxWidth, 0, uploadQuality)
				if err == nil {
					filename, err = saveUpload(processed, ".jpg")
				}
			}
			if err != nil {
				log.Printf("process/save photo %s: %v", fh.Filename, err)
				return
			}
			if err := db.AddPhoto(filename, caption, categoryID, placeID, pw, ph); err != nil {
				log.Printf("AddPhoto %s: %v", filename, err)
			}
		}()
	}

	// HTMX: возвращаем обновлённый список
	photos, _ := db.ListPhotos("", "")
	cats, _ := db.ListCategories()
	places, _ := db.ListPlaces()
	renderFragment(w, "admin/gallery_list.html", "gallery_list_content", page("Галерея", GalleryAdminData{Photos: photos, Categories: cats, Places: places}))
}

// ─────────────────────── Settings ───────────────────────

// GET /admin/settings
func AdminSettings(w http.ResponseWriter, r *http.Request) {
	settings := db.GetAllSettings()
	switch r.URL.Query().Get("pwerr") {
	case "short":
		settings["_pwerr"] = "Пароль должен быть не короче 8 символов"
	case "wrong":
		settings["_pwerr"] = "Текущий пароль неверный"
	}
	render(w, "admin/settings.html", page("Настройки", settings))
}

// POST /admin/settings
func SaveSettings(w http.ResponseWriter, r *http.Request) {
	fields := []string{
		"site_title", "site_desc", "home_bio",
		"social_github", "social_telegram", "social_instagram",
		"social_twitter", "social_bluesky", "social_mastodon",
		"social_vk", "social_linkedin", "social_email",
	}
	for _, key := range fields {
		db.SetSetting(key, r.FormValue(key))
	}
	if r.FormValue("resume_hidden") == "1" {
		db.SetSetting("resume_hidden", "1")
	} else {
		db.SetSetting("resume_hidden", "0")
	}
	LoadSettings()
	http.Redirect(w, r, "/admin/settings", http.StatusFound)
}

// POST /admin/settings/password
func ChangePassword(w http.ResponseWriter, r *http.Request) {
	current := r.FormValue("current_password")
	newPass := r.FormValue("new_password")
	if len(newPass) < 8 {
		http.Redirect(w, r, "/admin/settings?pwerr=short", http.StatusFound)
		return
	}

	login, hash, err := db.GetAdmin()
	if err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(current)) != nil {
		http.Redirect(w, r, "/admin/settings?pwerr=wrong", http.StatusFound)
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(newPass), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	if err := db.UpdatePassword(login, string(newHash)); err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}

	// Сбрасываем все сессии (включая текущую) — после смены пароля
	// чужие украденные куки перестают работать. Придётся войти заново.
	db.DeleteAllSessions()
	http.SetCookie(w, &http.Cookie{Name: "session", MaxAge: -1, Path: "/", HttpOnly: true, Secure: secureCookies, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/admin/login", http.StatusFound)
}

// POST /admin/settings/avatar
func UploadAvatar(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	if err := r.ParseMultipartForm(5 << 20); err != nil {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	defer r.MultipartForm.RemoveAll()

	filename, err := handleBoundedImageUpload(r, "avatar", coverMaxHeight)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if filename == "" {
		http.Error(w, "no file", http.StatusBadRequest)
		return
	}

	db.SetSetting("home_avatar", filename)
	LoadSettings()
	http.Redirect(w, r, "/admin/settings", http.StatusFound)
}

// POST /admin/settings/avatar/delete
func DeleteAvatar(w http.ResponseWriter, r *http.Request) {
	db.SetSetting("home_avatar", "")
	LoadSettings()
	http.Redirect(w, r, "/admin/settings", http.StatusFound)
}

// POST /admin/gallery/reorder
func ReorderPhotos(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	var ids []int
	for _, s := range r.Form["ids"] {
		if id, err := strconv.Atoi(s); err == nil {
			ids = append(ids, id)
		}
	}
	if err := db.UpdatePhotoOrder(ids); err != nil {
		http.Error(w, "db error", 500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /admin/gallery/{id}/delete
func DeletePhoto(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	filename, err := db.DeletePhoto(id)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	if err := os.Remove(filepath.Join(uploadsDir, filename)); err != nil {
		log.Printf("remove photo %s: %v", filename, err)
	}

	photos, _ := db.ListPhotos("", "")
	cats, _ := db.ListCategories()
	places, _ := db.ListPlaces()
	renderFragment(w, "admin/gallery_list.html", "gallery_list_content", page("Галерея", GalleryAdminData{Photos: photos, Categories: cats, Places: places}))
}

// renderGalleryMain рендерит фрагмент gallery_main_content для HTMX-ответов.
func renderGalleryMain(w http.ResponseWriter, r *http.Request) {
	photos, _ := db.ListPhotos("", "")
	cats, _ := db.ListCategories()
	places, _ := db.ListPlaces()
	renderFragment(w, "admin/gallery.html", "gallery_main_content",
		page("Галерея", GalleryAdminData{Photos: photos, Categories: cats, Places: places}))
}

// ─────────────────────── Places ───────────────────────

// POST /admin/places/new
func CreatePlace(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name required", 400)
		return
	}
	slug := slugify(name)
	if slug == "" {
		http.Error(w, "invalid name", 400)
		return
	}
	if err := db.CreatePlace(name, slug); err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		renderGalleryMain(w, r)
		return
	}
	http.Redirect(w, r, "/admin/gallery", http.StatusFound)
}

// POST /admin/places/{id}/delete
func DeletePlace(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := db.DeletePlace(id); err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		renderGalleryMain(w, r)
		return
	}
	http.Redirect(w, r, "/admin/gallery", http.StatusFound)
}

// POST /admin/gallery/{id}/place  — HTMX: update a photo's place inline
func UpdatePhotoPlace(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	placeID, _ := strconv.Atoi(r.FormValue("place_id"))
	if err := db.UpdatePhotoPlace(id, placeID); err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─────────────────────── Resume ───────────────────────

// GET /admin/resume
func AdminResume(w http.ResponseWriter, r *http.Request) {
	resume, err := db.GetResume()
	if err != nil {
		http.Error(w, "DB error", 500)
		return
	}
	render(w, "admin/resume.html", page("Резюме", resume))
}

// POST /admin/resume
func SaveResume(w http.ResponseWriter, r *http.Request) {
	bodyMD := r.FormValue("body")
	bodyHTML, err := RenderMD(bodyMD)
	if err != nil {
		http.Error(w, "markdown error", 500)
		return
	}
	db.UpdateResume(bodyMD, bodyHTML)
	http.Redirect(w, r, "/admin/resume", http.StatusFound)
}

// POST /admin/upload — uploads an image and returns its URL (for post editor)
func UploadImage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, header, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "no file", 400)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "read error", 500)
		return
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == "" {
		ext = ".webp"
	}
	if !isAllowedImageExt(ext) || !isAllowedImageContent(data) {
		http.Error(w, "unsupported file type", 400)
		return
	}

	var filename string
	if ext == ".gif" { // анимация — сохраняем как есть, без перекодирования
		filename, err = saveUpload(data, ext)
	} else {
		release := imgproc.Reserve()
		var processed []byte
		processed, _, _, err = imgproc.Process(data, uploadMaxWidth, 0, uploadQuality)
		release()
		if err == nil {
			filename, err = saveUpload(processed, ".jpg")
		}
	}
	if err != nil {
		log.Printf("upload image %s: %v", header.Filename, err)
		http.Error(w, "could not process image", 500)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("/uploads/" + filename))
}

// POST /admin/tags/delete — удаляет тег из автодополнения (и из всех постов,
// где он проставлен). Используется в редакторе для чистки опечаток.
func DeleteTag(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name required", 400)
		return
	}
	if err := db.DeleteTagByName(name); err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ─────────────────────── Helpers ───────────────────────

// parseTags разбирает строку вида "тег1, тег2, тег3" в слайс имён.
func loadAllTagNames() []string {
	tags, _ := db.ListAllTags()
	names := make([]string, 0, len(tags))
	for _, t := range tags {
		names = append(names, t.Name)
	}
	return names
}

func parseTags(s string) []string {
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// handleCoverUpload reads a "cover"-field image (post cover, series cover),
// resizes it capped on both dimensions (see coverMaxHeight) and saves it.
func handleCoverUpload(r *http.Request) (string, error) {
	return handleBoundedImageUpload(r, "cover", coverMaxHeight)
}

// handleBoundedImageUpload reads an image from the named multipart field,
// processes it through imgproc with the given height cap (0 = width-only),
// and saves the result. Returns "", nil if the field wasn't submitted.
func handleBoundedImageUpload(r *http.Request, field string, maxHeight int) (string, error) {
	file, header, err := r.FormFile(field)
	if err != nil {
		return "", nil // нет файла — не ошибка
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == "" {
		ext = ".webp"
	}
	if !isAllowedImageExt(ext) {
		return "", fmt.Errorf("unsupported file type: %s", ext)
	}

	data, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}
	if !isAllowedImageContent(data) {
		return "", fmt.Errorf("unsupported file type")
	}
	if ext == ".gif" { // анимация — сохраняем как есть, без перекодирования
		return saveUpload(data, ext)
	}
	release := imgproc.Reserve()
	processed, _, _, err := imgproc.Process(data, uploadMaxWidth, maxHeight, uploadQuality)
	release()
	if err != nil {
		return "", fmt.Errorf("process image: %w", err)
	}
	return saveUpload(processed, ".jpg")
}

// ─────────────────────── Categories ───────────────────────

// POST /admin/categories/new
func CreateCategory(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name required", 400)
		return
	}
	slug := slugify(name)
	if slug == "" {
		http.Error(w, "invalid name", 400)
		return
	}
	if err := db.CreateCategory(name, slug); err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		renderGalleryMain(w, r)
		return
	}
	http.Redirect(w, r, "/admin/gallery", http.StatusFound)
}

// POST /admin/categories/{id}/delete
func DeleteCategory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := db.DeleteCategory(id); err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		renderGalleryMain(w, r)
		return
	}
	http.Redirect(w, r, "/admin/gallery", http.StatusFound)
}

// ─────────────────────── Uploads browser ───────────────────────

type UploadFile struct {
	Filename string
	Size     int64
	ModTime  time.Time
	Usage    db.FileUsage
	Unused   bool
}

// GET /admin/uploads
func AdminUploads(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(uploadsDir)
	if err != nil {
		http.Error(w, "read dir error", 500)
		return
	}
	var files []UploadFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		info, err := entry.Info()
		if err != nil {
			continue
		}
		usage, err := db.GetFileUsage(name)
		if err != nil {
			log.Printf("GetFileUsage %s: %v", name, err)
			continue
		}
		files = append(files, UploadFile{
			Filename: name,
			Size:     info.Size(),
			ModTime:  info.ModTime(),
			Usage:    usage,
			Unused:   !usage.InGallery && len(usage.PostTitles) == 0,
		})
	}
	render(w, "admin/uploads.html", page("Файлы", files))
}

// POST /admin/uploads/{filename}/delete
func DeleteUploadFile(w http.ResponseWriter, r *http.Request) {
	filename := filepath.Base(r.PathValue("filename"))
	usage, err := db.GetFileUsage(filename)
	if err != nil {
		http.Error(w, "db error", 500)
		return
	}
	if usage.InGallery || len(usage.PostTitles) > 0 {
		http.Error(w, "file is in use", 409)
		return
	}
	if err := os.Remove(filepath.Join(uploadsDir, filename)); err != nil {
		log.Printf("delete upload %s: %v", filename, err)
		http.Error(w, "remove error", 500)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/admin/uploads", http.StatusFound)
}

// POST /admin/gallery/{id}/category  — HTMX: update a photo's category inline
func UpdatePhotoCategory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	categoryID, _ := strconv.Atoi(r.FormValue("category_id"))
	if err := db.UpdatePhotoCategory(id, categoryID); err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

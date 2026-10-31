package handlers

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/demen1n/dsite/internal/db"
	"github.com/demen1n/dsite/internal/imgproc"
	"github.com/demen1n/dsite/internal/typo"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

var (
	baseTmpl       *template.Template
	adminBaseTmpl  *template.Template
	templatesDir   string
	uploadsDir     string
	siteTitle      string     // SITE_TITLE; the site_title setting overrides it
	siteDesc       string     // SITE_DESC; the site_desc setting overrides it
	siteURL        string     // canonical base URL, e.g. https://demenin.ru
	siteLoc        = time.UTC // time zone dates are shown in (SITE_TZ)
	secureCookies  bool
	trustedProxy   bool
	md             goldmark.Markdown
	funcMap        template.FuncMap
	staticVersions map[string]string // filename -> content hash, for cache-busting /static URLs
)

// ───── Upload allowlist ─────

var allowedImageExts = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
	".webp": true,
}

func isAllowedImageExt(ext string) bool {
	return allowedImageExts[strings.ToLower(ext)]
}

// isAllowedImageContent checks magic bytes to verify the file is actually an image.
func isAllowedImageContent(data []byte) bool {
	ct := http.DetectContentType(data)
	switch ct {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	}
	// http.DetectContentType doesn't reliably detect WebP/AVIF; check magic bytes.
	if len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return true
	}
	return false
}

// ───── Login rate limiter ─────

const (
	maxLoginAttempts = 5
	loginWindow      = 15 * time.Minute
	lockoutDuration  = 15 * time.Minute
)

type loginAttempt struct {
	count    int
	firstAt  time.Time
	lockedAt time.Time
}

var (
	loginMu       sync.Mutex
	loginAttempts = map[string]*loginAttempt{}
)

func clientIP(r *http.Request) string {
	if trustedProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// Берём последний элемент: его дописал наш прокси (Caddy appends),
			// а первые может прислать сам клиент и подделать IP.
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func LoginAllowed(r *http.Request) bool {
	ip := clientIP(r)
	loginMu.Lock()
	defer loginMu.Unlock()
	a := loginAttempts[ip]
	if a == nil {
		return true
	}
	if !a.lockedAt.IsZero() {
		if time.Since(a.lockedAt) > lockoutDuration {
			delete(loginAttempts, ip)
			return true
		}
		return false
	}
	return true
}

func RecordLoginFailure(r *http.Request) {
	ip := clientIP(r)
	loginMu.Lock()
	defer loginMu.Unlock()
	a := loginAttempts[ip]
	if a == nil {
		a = &loginAttempt{}
		loginAttempts[ip] = a
	}
	now := time.Now()
	if time.Since(a.firstAt) > loginWindow {
		a.count = 0
		a.firstAt = now
	}
	a.count++
	if a.count >= maxLoginAttempts {
		a.lockedAt = now
	}
}

func RecordLoginSuccess(r *http.Request) {
	ip := clientIP(r)
	loginMu.Lock()
	defer loginMu.Unlock()
	delete(loginAttempts, ip)
}

func cleanLoginAttempts() {
	loginMu.Lock()
	defer loginMu.Unlock()
	now := time.Now()
	for ip, a := range loginAttempts {
		if a.lockedAt.IsZero() && now.Sub(a.firstAt) > loginWindow {
			delete(loginAttempts, ip)
		} else if !a.lockedAt.IsZero() && now.Sub(a.lockedAt) > lockoutDuration {
			delete(loginAttempts, ip)
		}
	}
}

func Init(tmplDir, uploads, title, desc, siteBaseURL string, secure, trusted bool) {
	templatesDir = tmplDir
	uploadsDir = uploads
	siteTitle = title
	siteDesc = desc
	curSettings.Store(nil) // drop a snapshot built from another config (tests)
	siteURL = strings.TrimRight(siteBaseURL, "/")
	secureCookies = secure
	trustedProxy = trusted
	loadStaticVersions("./static")

	md = goldmark.New(
		goldmark.WithExtensions(extension.GFM, extension.Table),
		goldmark.WithRendererOptions(html.WithUnsafe()),
	)

	funcMap = template.FuncMap{
		"safeHTML":     func(s string) template.HTML { return template.HTML(s) },
		"fmtDate":      ruDate,
		"fmtDateShort": func(t time.Time) string { return t.In(siteLoc).Format("02.01.2006") },
		"relDate":      relDate,
		"siteTime":     func(t time.Time) time.Time { return t.In(siteLoc) },
		"excerpt":      excerpt,
		"postWord":     postWord,
		"hasMore":      func(s string) bool { return strings.Contains(s, "<!--more-->") },
		"beforeMore": func(s string) template.HTML {
			if i := strings.Index(s, "<!--more-->"); i >= 0 {
				s = s[:i]
			}
			return template.HTML(responsiveImages(s))
		},
		"fmtSize": func(n int64) string {
			switch {
			case n >= 1<<20:
				return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
			case n >= 1<<10:
				return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
			default:
				return fmt.Sprintf("%d B", n)
			}
		},
		"tagSlug":     db.TagSlug,
		"thumbWidth":  func() int { return thumbWidth },
		"thumbSrcset": thumbSrcset,
		"thumbSrc":    thumbSrc,
		"thumbRatio":  thumbRatio,
		"thumbAttrs":  thumbAttrs,
		"coverSizes":  func() string { return coverImgSizes },
		// postHTML — HTML поста с адаптивными картинками (см. responsiveImages).
		"postHTML": func(s string) template.HTML { return template.HTML(responsiveImages(s)) },
		"add":      func(a, b int) int { return a + b },
		"sub":      func(a, b int) int { return a - b },
		"join":     strings.Join,
		"json": func(v any) (template.JS, error) {
			b, err := json.Marshal(v)
			return template.JS(b), err
		},
		"static": func(name string) string {
			if v, ok := staticVersions[name]; ok {
				return "/static/" + name + "?v=" + v
			}
			return "/static/" + name
		},
	}

	var err error
	baseTmpl, err = template.New("").Funcs(funcMap).
		ParseFiles(filepath.Join(templatesDir, "base.html"))
	if err != nil {
		log.Fatalf("parse base.html: %v", err)
	}
	adminBaseTmpl, err = template.New("").Funcs(funcMap).
		ParseFiles(filepath.Join(templatesDir, "admin", "base.html"))
	if err != nil {
		log.Fatalf("parse admin/base.html: %v", err)
	}
	log.Println("Templates loaded from", templatesDir)
	loadThumbIndex()

	go func() {
		for range time.Tick(10 * time.Minute) {
			cleanLoginAttempts()
		}
	}()
}

// loadStaticVersions hashes every file in the static dir so templates can
// append a content-derived ?v= query param (see the "static" template func).
// Without it, browsers keep serving a stale cached /static/x.js for up to
// an hour after deploy (Cache-Control: max-age=3600) since the URL itself
// never changes.
func loadStaticVersions(dir string) {
	staticVersions = map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("static versions: %v", err)
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		sum := sha256.Sum256(data)
		staticVersions[e.Name()] = hex.EncodeToString(sum[:])[:8]
	}
}

// render рендерит страницу.
// Схема: base.html содержит {{block "content"}} / {{block "admin_content"}}.
// Файлы страниц содержат только {{define "content"}} без вызова базы.
// Мы клонируем базу и парсим файл страницы в клон — {{define}} переопределяет {{block}}.
// Standalone-шаблоны (login, setup) не используют базу, исполняются по имени define.
func render(w http.ResponseWriter, name string, data any) {
	renderStatus(w, http.StatusOK, name, data)
}

// renderStatus — render с произвольным HTTP-статусом (например, 404).
// Страница исполняется в буфер, чтобы статус уходил только после успешного
// рендера: иначе ошибка шаблона посреди ответа дала бы обрывок страницы
// с кодом 200 вместо чистого 500.
func renderStatus(w http.ResponseWriter, status int, name string, data any) {
	pageFile := filepath.Join(templatesDir, filepath.FromSlash(name))

	var base *template.Template
	var entrypoint string
	if strings.HasPrefix(name, "admin/") {
		base = adminBaseTmpl
		entrypoint = "admin_base"
	} else {
		base = baseTmpl
		entrypoint = "base"
	}

	t, err := base.Clone()
	if err != nil {
		log.Printf("clone for %s: %v", name, err)
		http.Error(w, "Internal Server Error", 500)
		return
	}
	if _, err = t.ParseFiles(pageFile); err != nil {
		log.Printf("parse %s: %v", name, err)
		http.Error(w, "Internal Server Error", 500)
		return
	}

	var buf bytes.Buffer
	if err = t.ExecuteTemplate(&buf, entrypoint, data); err != nil {
		log.Printf("execute %s (%s): %v", name, entrypoint, err)
		http.Error(w, "Internal Server Error", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w) // ошибка тут — оборванное клиентом соединение, делать с ней нечего
}

// NotFound отдаёт оформленную страницу 404: в админке (для залогиненного) —
// в админском лэйауте, иначе — в публичном. HTMX-запросам уходит plain text:
// htmx по умолчанию не свапает 4xx, а целая страница внутри фрагмента
// всё равно была бы не к месту.
func NotFound(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") != "" {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	pd := page("Страница не найдена", nil)
	if isAdminPath(r.URL.Path) && IsAuthed(r) {
		renderStatus(w, http.StatusNotFound, "admin/404.html", pd)
		return
	}
	renderStatus(w, http.StatusNotFound, "404.html", pd)
}

func isAdminPath(path string) bool {
	return path == "/admin" || strings.HasPrefix(path, "/admin/")
}

// renderFragment рендерит отдельный шаблон без базового лэйаута.
// Используется для HTMX-фрагментов (gallery_list, preview и т.д.).
// fragmentName — имя {{define}} внутри файла.
func renderFragment(w http.ResponseWriter, file, fragmentName string, data any) {
	pageFile := filepath.Join(templatesDir, filepath.FromSlash(file))
	t, err := template.New("").Funcs(funcMap).ParseFiles(pageFile)
	if err != nil {
		log.Printf("parse fragment %s: %v", file, err)
		http.Error(w, "Internal Server Error", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err = t.ExecuteTemplate(w, fragmentName, data); err != nil {
		log.Printf("execute fragment %s/%s: %v", file, fragmentName, err)
		http.Error(w, "Internal Server Error", 500)
	}
}

// markdownVersion — версия рендерера RenderMD. Сохранённый HTML постов,
// серий и резюме сделан тем рендерером, что был на момент сохранения;
// поднимите версию, когда меняется вывод RenderMD (новое расширение,
// правило типографа), — RerenderMarkdown на старте перегенерирует его.
const markdownVersion = "3"

// RenderMD превращает markdown в HTML, оформляет картинки с подписью
// (см. figures) и применяет типографику.
func RenderMD(src string) (string, error) {
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return "", err
	}
	return typo.HTML(figures(buf.String())), nil
}

var (
	reImgPara  = regexp.MustCompile(`<p>((?:<img\s[^>]*>\s*)+)</p>`)
	reImgInPar = regexp.MustCompile(`<img\s[^>]*>`)
	reImgTitle = regexp.MustCompile(`\stitle="([^"]*)"`)
)

// figures превращает картинки с title — ![](фото.jpg "Подпись") — в
// <figure> с <figcaption>. Подпись берётся из title, а не из alt: alt —
// текст для незрячих и поисковиков, его не обязательно показывать.
// Картинки, идущие подряд строками, markdown кладёт в один абзац; такой
// абзац, если в нём есть подписанная картинка, разбирается на отдельные
// блоки (<figure> внутри <p> — невалидный HTML).
func figures(html string) string {
	return reImgPara.ReplaceAllStringFunc(html, func(para string) string {
		if !reImgTitle.MatchString(para) {
			return para
		}
		imgs := reImgInPar.FindAllString(para, -1)
		for i, img := range imgs {
			m := reImgTitle.FindStringSubmatchIndex(img)
			if m == nil {
				imgs[i] = "<p>" + img + "</p>"
				continue
			}
			caption := img[m[2]:m[3]]
			imgs[i] = "<figure>" + img[:m[0]] + img[m[1]:] + "<figcaption>" + caption + "</figcaption></figure>"
		}
		return strings.Join(imgs, "\n")
	})
}

// RerenderMarkdown перегенерирует сохранённый HTML, если он сделан
// рендерером другой версии (см. markdownVersion). Вызывать после Init.
func RerenderMarkdown() error {
	if db.GetAllSettings()["markdown_version"] == markdownVersion {
		return nil
	}
	n, err := db.RerenderMarkdown(RenderMD)
	if err != nil {
		return err
	}
	log.Printf("markdown: re-rendered %d item(s) for renderer version %s", n, markdownVersion)
	return db.SetSetting("markdown_version", markdownVersion)
}

type PageData struct {
	Title         string
	SiteTitle     string
	SiteDesc      string
	HomeBio       template.HTML
	HomeAvatar    string // filename, served from /uploads/
	Socials       map[string]string
	Data          any
	AllTags       []string
	AllSeries     []db.Series
	OGDescription string
	OGImage       string // absolute URL
	OGType        string // og:type; пусто — article при OGDescription, иначе website
	Canonical     string
	Robots        string // <meta name="robots">; пусто — тег не выводится
	ResumeHidden  bool
}

// siteSettings is an immutable snapshot of the admin-editable settings.
// LoadSettings builds a fresh one and swaps the pointer, so a request that
// reads it concurrently with a save sees either the old or the new values,
// never a mix (and without a data race).
type siteSettings struct {
	title        string
	desc         string
	homeBio      template.HTML
	homeAvatar   string // filename of profile photo, served from /uploads/
	socials      map[string]string
	resumeHidden bool
}

var curSettings atomic.Pointer[siteSettings]

// settings returns the current snapshot; before the first LoadSettings it
// falls back to the env defaults from Init.
func settings() *siteSettings {
	if st := curSettings.Load(); st != nil {
		return st
	}
	return &siteSettings{title: siteTitle, desc: siteDesc}
}

func LoadSettings() {
	s := db.GetAllSettings()
	st := &siteSettings{
		title:      siteTitle,
		desc:       siteDesc,
		homeAvatar: s["home_avatar"],
		socials: map[string]string{
			"github":    s["social_github"],
			"telegram":  s["social_telegram"],
			"instagram": s["social_instagram"],
			"twitter":   s["social_twitter"],
			"bluesky":   s["social_bluesky"],
			"mastodon":  s["social_mastodon"],
			"vk":        s["social_vk"],
			"linkedin":  s["social_linkedin"],
			"email":     s["social_email"],
		},
		resumeHidden: s["resume_hidden"] == "1",
	}
	if v := s["site_title"]; v != "" {
		st.title = v
	}
	if v := s["site_desc"]; v != "" {
		st.desc = v
	}
	if v := s["home_bio"]; v != "" {
		if rendered, err := RenderMD(v); err == nil {
			h := strings.TrimSpace(rendered)
			if strings.HasPrefix(h, "<p>") && strings.HasSuffix(h, "</p>") {
				h = strings.TrimSpace(h[3 : len(h)-4])
			}
			st.homeBio = template.HTML(h)
		}
	}
	curSettings.Store(st)
}

func ResumeHidden() bool { return settings().resumeHidden }

func page(title string, data any) PageData {
	st := settings()
	return PageData{Title: title, SiteTitle: st.title, SiteDesc: st.desc, HomeBio: st.homeBio, HomeAvatar: st.homeAvatar, Socials: st.socials, Data: data, ResumeHidden: st.resumeHidden}
}

func baseURL(r *http.Request) string {
	if siteURL != "" {
		return siteURL
	}
	scheme := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
		scheme = "http"
	}
	return scheme + "://" + r.Host
}

// ───── Session ─────

const sessionDuration = 30 * 24 * time.Hour

func NewSession() (string, error) {
	b := make([]byte, 16)
	rand.Read(b)
	token := hex.EncodeToString(b)
	if err := db.CreateSession(token, time.Now().Add(sessionDuration)); err != nil {
		return "", err
	}
	return token, nil
}

func IsAuthed(r *http.Request) bool {
	c, err := r.Cookie("session")
	if err != nil {
		return false
	}
	return db.SessionValid(c.Value)
}

func RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !IsAuthed(r) {
			http.Redirect(w, r, "/admin/login", http.StatusFound)
			return
		}
		next(w, r)
	}
}

func DeleteSession(token string) {
	if err := db.DeleteSession(token); err != nil {
		log.Printf("DeleteSession: %v", err)
	}
}

// ───── File utils ─────

// BackfillPhotoDimensions reads dimension from files for photos that have none stored.
// Runs in background so it doesn't block the response.
func BackfillPhotoDimensions(photos []db.Photo) {
	var missing []db.Photo
	for _, p := range photos {
		if p.Width == 0 || p.Height == 0 {
			missing = append(missing, p)
		}
	}
	if len(missing) == 0 {
		return
	}
	go func() {
		for _, p := range missing {
			path := filepath.Join(uploadsDir, p.Filename)
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			w, h, err := imgproc.Dimensions(data)
			if err != nil {
				continue
			}
			if err := db.UpdatePhotoDimensions(p.ID, w, h); err != nil {
				log.Printf("backfill dimensions for photo %d: %v", p.ID, err)
			}
		}
	}()
}

func saveUpload(data []byte, ext string) (string, error) {
	b := make([]byte, 8)
	rand.Read(b)
	filename := fmt.Sprintf("%d_%s%s", time.Now().Unix(), hex.EncodeToString(b), ext)
	path := filepath.Join(uploadsDir, filename)
	//nolint:gosec // G703: filename is server-generated (timestamp+random hex);
	// its only caller-supplied part, ext, comes from filepath.Ext (which by
	// construction can't contain a path separator) and is checked against
	// isAllowedImageExt before saveUpload is ever called.
	return filename, os.WriteFile(path, data, 0600)
}

var cyrillicMap = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "yo",
	'ж': "zh", 'з': "z", 'и': "i", 'й': "j", 'к': "k", 'л': "l", 'м': "m",
	'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u",
	'ф': "f", 'х': "kh", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "shch",
	'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
}

func transliterate(s string) string {
	var b strings.Builder
	for _, r := range s {
		if lat, ok := cyrillicMap[r]; ok {
			b.WriteString(lat)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func slugify(s string) string {
	s = strings.ToLower(s)
	s = transliterate(s)
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r == ' ' || r == '-' || r == '_':
			return '-'
		default:
			return -1
		}
	}, s)
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

var reHTMLTag = regexp.MustCompile(`<[^>]+>`)
var reImgSrc = regexp.MustCompile(`<img[^>]+src="([^"]*)"`)

// firstImageSrc возвращает src первой картинки в теле поста — используется
// как замена обложке для карточек серии, когда у поста своей обложки нет.
func firstImageSrc(html string) string {
	m := reImgSrc.FindStringSubmatch(html)
	if m == nil {
		return ""
	}
	return m[1]
}

// allImageSrcs возвращает src всех картинок в теле поста — используется для
// серий, собирающих фото со всех своих постов в одну галерею.
func allImageSrcs(html string) []string {
	matches := reImgSrc.FindAllStringSubmatch(html, -1)
	srcs := make([]string, 0, len(matches))
	for _, m := range matches {
		srcs = append(srcs, m[1])
	}
	return srcs
}

// excerpt снимает HTML-теги и возвращает первые ~280 символов, обрывая по слову.
func excerpt(html string) string {
	plain := reHTMLTag.ReplaceAllString(html, "")
	plain = strings.TrimSpace(plain)
	const maxRunes = 280
	if utf8.RuneCountInString(plain) <= maxRunes {
		return plain
	}
	runes := []rune(plain)
	cut := string(runes[:maxRunes])
	// обрываем по последнему пробелу
	if i := strings.LastIndexByte(cut, ' '); i > 0 {
		cut = cut[:i]
	}
	return cut + "…"
}

// SetSiteLocation задаёт часовой пояс, в котором сайт показывает даты (SITE_TZ).
func SetSiteLocation(loc *time.Location) { siteLoc = loc }

// relDate возвращает относительную дату на русском для свежих записей,
// иначе — абсолютную дату.
func relDate(t time.Time) string { return relDateAt(t, time.Now()) }

// relDateAt — relDate относительно момента now. «Сегодня»/«вчера» считаются
// по календарным дням в часовом поясе сайта, а не по 24-часовым интервалам:
// пост, вышедший вчера в 23:00, — «вчера», даже если прошёл всего час.
func relDateAt(t, now time.Time) string {
	days := calendarDays(t.In(siteLoc), now.In(siteLoc))
	switch {
	case days <= 0:
		return "сегодня"
	case days == 1:
		return "вчера"
	case days < 7:
		return fmt.Sprintf("%d %s назад", days, dayWord(days))
	case days < 30:
		w := days / 7
		return fmt.Sprintf("%d %s назад", w, weekWord(w))
	default:
		return ruDate(t)
	}
}

var ruMonths = [...]string{"января", "февраля", "марта", "апреля", "мая", "июня",
	"июля", "августа", "сентября", "октября", "ноября", "декабря"}

// ruDate — дата по-русски: «15 июня 2026». time.Format знает только
// английские названия месяцев.
func ruDate(t time.Time) string {
	t = t.In(siteLoc)
	return fmt.Sprintf("%d %s %d", t.Day(), ruMonths[t.Month()-1], t.Year())
}

// calendarDays — сколько смен даты между from и to (оба уже в нужном поясе).
func calendarDays(from, to time.Time) int {
	y1, m1, d1 := from.Date()
	y2, m2, d2 := to.Date()
	return int(time.Date(y2, m2, d2, 0, 0, 0, 0, time.UTC).Sub(time.Date(y1, m1, d1, 0, 0, 0, 0, time.UTC)).Hours() / 24)
}

func dayWord(n int) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return "день"
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20):
		return "дня"
	default:
		return "дней"
	}
}

func weekWord(n int) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return "неделю"
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20):
		return "недели"
	default:
		return "недель"
	}
}

func postWord(n int) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return "пост"
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20):
		return "поста"
	default:
		return "постов"
	}
}

// setupToken guards /admin/setup while there's no admin yet: without it,
// whoever finds a freshly deployed site first could claim it. Generated per
// process and only printed to the server log, which only the operator sees.
var setupToken atomic.Value // string; "" once the admin exists

func currentSetupToken() string {
	t, _ := setupToken.Load().(string)
	return t
}

func EnsureAdminExists() {
	exists, err := db.UserExists()
	if err != nil {
		log.Fatal("check user:", err)
	}
	if !exists {
		b := make([]byte, 16)
		rand.Read(b)
		setupToken.Store(hex.EncodeToString(b))
		log.Printf("⚠️  No admin user found. Create one at /admin/setup?token=%s", currentSetupToken())
	}
}

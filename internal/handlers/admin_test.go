package handlers

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/demen1n/dsite/internal/db"
)

// postForm шлёт multipart-форму редактора прямо в хендлер (без RequireAuth
// и csrfCheck — они проверяются отдельно).
func postForm(t *testing.T, h http.HandlerFunc, pattern, target string, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatalf("WriteField: %v", err)
		}
	}
	mw.Close()
	req := httptest.NewRequest("POST", target, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	mux := http.NewServeMux()
	mux.HandleFunc(pattern, h)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestEditorPublishedAt(t *testing.T) {
	setupPublic(t)
	const editPattern = "POST /admin/posts/{id}/edit"
	fields := func(publishedAt string, published bool) map[string]string {
		f := map[string]string{"title": "Post", "slug": "post", "body": "x", "published_at": publishedAt}
		if published {
			f["published"] = "1"
		}
		return f
	}

	// Создание с ручной датой.
	manual := time.Date(2025, 6, 1, 3, 0, 0, 0, time.UTC)
	if rec := postForm(t, CreatePost, "POST /admin/posts/new", "/admin/posts/new", fields("2025-06-01T03:00:00.000Z", true)); rec.Code != http.StatusFound {
		t.Fatalf("create: status = %d", rec.Code)
	}
	p, err := db.GetPostBySlug("post")
	if err != nil {
		t.Fatalf("GetPostBySlug: %v", err)
	}
	if !p.PublishedAt.Equal(manual) {
		t.Errorf("create with date: PublishedAt = %v, want %v", p.PublishedAt, manual)
	}
	target := "/admin/posts/" + strconv.Itoa(p.ID) + "/edit"

	// Пустое поле дату не трогает — даже через черновик и обратно.
	postForm(t, UpdatePost, editPattern, target, fields("", false))
	postForm(t, UpdatePost, editPattern, target, fields("", true))
	if p, _ := db.GetPostByID(p.ID); !p.PublishedAt.Equal(manual) {
		t.Errorf("empty field: PublishedAt = %v, want unchanged %v", p.PublishedAt, manual)
	}

	// Ручная правка при редактировании.
	edited := time.Date(2024, 12, 31, 21, 15, 0, 0, time.UTC)
	if rec := postForm(t, UpdatePost, editPattern, target, fields("2024-12-31T21:15:00.000Z", true)); rec.Code != http.StatusFound {
		t.Fatalf("update: status = %d", rec.Code)
	}
	if p, _ := db.GetPostByID(p.ID); !p.PublishedAt.Equal(edited) {
		t.Errorf("edit date: PublishedAt = %v, want %v", p.PublishedAt, edited)
	}

	// Мусор — 400, пост не меняется.
	if rec := postForm(t, UpdatePost, editPattern, target, map[string]string{"title": "Changed", "published_at": "31.12.2024"}); rec.Code != http.StatusBadRequest {
		t.Errorf("garbage date: status = %d, want 400", rec.Code)
	}
	if p, _ := db.GetPostByID(p.ID); p.Title != "Post" {
		t.Errorf("garbage date: post changed to %q", p.Title)
	}
}

// postURLForm шлёт обычную (urlencoded) форму — так браузер отправляет
// формы массовых действий.
func postURLForm(h http.HandlerFunc, pattern, target string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux := http.NewServeMux()
	mux.HandleFunc(pattern, h)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestBulkSeriesHandlers(t *testing.T) {
	setupPublic(t)
	sid, _ := db.CreateSeries("Trip", "trip", "", "", "", false, true, false, false)
	other, _ := db.CreateSeries("Other", "other", "", "", "", false, true, false, false)
	p1, _ := db.CreatePost("p1", "First post", "", "", "", true, 0)
	p2, _ := db.CreatePost("p2", "Second post", "", "", "", false, 0)
	p3, _ := db.CreatePost("p3", "Third post", "", "", "", true, int(other))
	ids := func(ids ...int64) []string {
		var s []string
		for _, id := range ids {
			s = append(s, strconv.FormatInt(id, 10))
		}
		return s
	}
	seriesOf := func(id int64) int {
		p, _ := db.GetPostByID(int(id))
		return p.SeriesID
	}
	sPath := "/admin/series/" + strconv.FormatInt(sid, 10)

	// Вариант A: добавить со страницы серии.
	rec := postURLForm(AddPostsToSeries, "POST /admin/series/{id}/posts", sPath+"/posts",
		url.Values{"post_ids": ids(p1, p2)})
	if rec.Code != http.StatusFound || seriesOf(p1) != int(sid) || seriesOf(p2) != int(sid) {
		t.Fatalf("add: status %d, p1=%d p2=%d", rec.Code, seriesOf(p1), seriesOf(p2))
	}

	// Страница серии: посты серии и кандидаты (с текущей серией) на месте.
	page := serve(EditSeriesForm, "GET /admin/series/{id}/edit", sPath+"/edit").Body.String()
	for _, want := range []string{"First post", "Second post", "Third post", "Other", sPath + "/posts/remove"} {
		if !strings.Contains(page, want) {
			t.Errorf("series edit page missing %q", want)
		}
	}

	// Вариант A: убрать — p3 не в этой серии, его не трогаем.
	rec = postURLForm(RemovePostsFromSeries, "POST /admin/series/{id}/posts/remove", sPath+"/posts/remove",
		url.Values{"post_ids": ids(p2, p3)})
	if rec.Code != http.StatusFound || seriesOf(p2) != 0 || seriesOf(p3) != int(other) || seriesOf(p1) != int(sid) {
		t.Errorf("remove: status %d, p1=%d p2=%d p3=%d", rec.Code, seriesOf(p1), seriesOf(p2), seriesOf(p3))
	}

	// Вариант B: из списка постов — перенести и убрать.
	rec = postURLForm(BulkSetPostsSeries, "POST /admin/posts/series", "/admin/posts/series",
		url.Values{"post_ids": ids(p1, p3), "series_id": {strconv.FormatInt(other, 10)}})
	if rec.Code != http.StatusFound || seriesOf(p1) != int(other) || seriesOf(p3) != int(other) {
		t.Errorf("bulk move: status %d, p1=%d p3=%d", rec.Code, seriesOf(p1), seriesOf(p3))
	}
	rec = postURLForm(BulkSetPostsSeries, "POST /admin/posts/series", "/admin/posts/series",
		url.Values{"post_ids": ids(p1), "series_id": {"0"}})
	if rec.Code != http.StatusFound || seriesOf(p1) != 0 {
		t.Errorf("bulk clear: status %d, p1=%d", rec.Code, seriesOf(p1))
	}

	// Несуществующая серия — 400, ничего не меняется.
	rec = postURLForm(BulkSetPostsSeries, "POST /admin/posts/series", "/admin/posts/series",
		url.Values{"post_ids": ids(p3), "series_id": {"9999"}})
	if rec.Code != http.StatusBadRequest || seriesOf(p3) != int(other) {
		t.Errorf("unknown series: status %d, p3=%d", rec.Code, seriesOf(p3))
	}
	if rec := postURLForm(AddPostsToSeries, "POST /admin/series/{id}/posts", "/admin/series/9999/posts",
		url.Values{"post_ids": ids(p1)}); rec.Code != http.StatusNotFound {
		t.Errorf("add to unknown series: status %d, want 404", rec.Code)
	}

	// Список постов: колонка серии и форма массового переноса.
	index := serve(AdminIndex, "GET /admin", "/admin").Body.String()
	for _, want := range []string{`action="/admin/posts/series"`, `form="bulk-form"`, "Other"} {
		if !strings.Contains(index, want) {
			t.Errorf("admin index missing %q", want)
		}
	}
}

func TestBulkSeriesValidation(t *testing.T) {
	setupPublic(t)
	sid, _ := db.CreateSeries("Trip", "trip", "", "", "", false, true, false, false)
	pid, _ := db.CreatePost("p", "P", "", "", "", true, int(sid))
	keep := url.Values{"post_ids": {strconv.FormatInt(pid, 10)}}

	// Мусорный или отрицательный series_id в варианте B — 400.
	for _, v := range []string{"abc", "-1", ""} {
		form := url.Values{"post_ids": keep["post_ids"], "series_id": {v}}
		if rec := postURLForm(BulkSetPostsSeries, "POST /admin/posts/series", "/admin/posts/series", form); rec.Code != http.StatusBadRequest {
			t.Errorf("series_id=%q: status %d, want 400", v, rec.Code)
		}
	}

	// Нечисловой и несуществующий id серии в пути — 404 во всех хендлерах.
	for _, c := range []struct {
		name    string
		h       http.HandlerFunc
		pattern string
		suffix  string
	}{
		{"add", AddPostsToSeries, "POST /admin/series/{id}/posts", "/posts"},
		{"remove", RemovePostsFromSeries, "POST /admin/series/{id}/posts/remove", "/posts/remove"},
	} {
		for _, id := range []string{"abc", "9999"} {
			if rec := postURLForm(c.h, c.pattern, "/admin/series/"+id+c.suffix, keep); rec.Code != http.StatusNotFound {
				t.Errorf("%s, series %s: status %d, want 404", c.name, id, rec.Code)
			}
		}
	}
	for _, id := range []string{"abc", "9999"} {
		if rec := serve(EditSeriesForm, "GET /admin/series/{id}/edit", "/admin/series/"+id+"/edit"); rec.Code != http.StatusNotFound {
			t.Errorf("edit page, series %s: status %d, want 404", id, rec.Code)
		}
	}

	// Мусор в post_ids пропускается, валидные id обрабатываются.
	form := url.Values{"post_ids": {"x", "-5", "0", strconv.FormatInt(pid, 10)}, "series_id": {"0"}}
	if rec := postURLForm(BulkSetPostsSeries, "POST /admin/posts/series", "/admin/posts/series", form); rec.Code != http.StatusFound {
		t.Fatalf("garbage post_ids: status %d, want 302", rec.Code)
	}
	if p, _ := db.GetPostByID(int(pid)); p.SeriesID != 0 {
		t.Errorf("garbage post_ids: valid post not processed, series %d", p.SeriesID)
	}
}

func TestScheduledPost(t *testing.T) {
	setupPublic(t)
	future := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fields := map[string]string{"title": "Завтра", "slug": "zavtra", "body": "x", "published": "1", "published_at": future}
	if rec := postForm(t, CreatePost, "POST /admin/posts/new", "/admin/posts/new", fields); rec.Code != http.StatusFound {
		t.Fatalf("create: status = %d", rec.Code)
	}

	if rec := serve(ViewPost, "GET /post/{slug}", "/post/zavtra"); rec.Code != 404 {
		t.Errorf("scheduled post page: status = %d, want 404", rec.Code)
	}
	// И по старому адресу — 404, а не редирект на ещё не вышедший пост.
	p, err := db.GetPostBySlug("zavtra")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpdatePost(p.ID, "zavtra-2", p.Title, p.BodyMD, p.BodyHTML, "", true, 0); err != nil {
		t.Fatal(err)
	}
	if rec := serve(ViewPost, "GET /post/{slug}", "/post/zavtra"); rec.Code != 404 {
		t.Errorf("old slug of a scheduled post: status = %d, want 404", rec.Code)
	}
	for _, pg := range []struct {
		path string
		h    http.HandlerFunc
	}{{"/blog", Index}, {"/feed.xml", Feed}, {"/sitemap.xml", Sitemap}} {
		if rec := serve(pg.h, "GET "+pg.path, pg.path); strings.Contains(rec.Body.String(), "zavtra") {
			t.Errorf("%s lists the scheduled post", pg.path)
		}
	}

	rec := serve(AdminIndex, "GET /admin", "/admin")
	body := rec.Body.String()
	if !strings.Contains(body, "Запланирован") {
		t.Error("admin list doesn't mark the post as scheduled")
	}
	if strings.Contains(body, `href="/post/zavtra" target="_blank"`) {
		t.Error("admin list links to the not-yet-public post")
	}
}

// Удаление записи галереи не должно удалять файл, который ещё где-то
// используется (AddToGallery переиспользует файлы постов).
func TestDeletePhotoKeepsSharedFile(t *testing.T) {
	setupPublic(t)
	for _, fn := range []string{"shared.jpg", "lonely.jpg"} {
		if err := os.WriteFile(filepath.Join(uploadsDir, fn), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := db.AddPhoto(fn, "", 0, 0, 1, 1); err != nil {
			t.Fatalf("AddPhoto: %v", err)
		}
	}
	if _, err := db.CreatePost("p", "Post", "![](/uploads/shared.jpg)", "", "", true, 0); err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	photos, _ := db.ListPhotos("", "")
	for _, p := range photos {
		rec := postURLForm(DeletePhoto, "POST /admin/gallery/{id}/delete",
			"/admin/gallery/"+strconv.Itoa(p.ID)+"/delete", url.Values{})
		if rec.Code != http.StatusOK {
			t.Fatalf("delete %s: status %d", p.Filename, rec.Code)
		}
	}
	if _, err := os.Stat(filepath.Join(uploadsDir, "shared.jpg")); err != nil {
		t.Errorf("shared.jpg (used by a post) was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(uploadsDir, "lonely.jpg")); !os.IsNotExist(err) {
		t.Errorf("lonely.jpg (unused) should be removed, stat err = %v", err)
	}
}

func TestCleanSlug(t *testing.T) {
	for _, c := range []struct{ input, title, current, want string }{
		{"", "Привет мир", "", "privet-mir"},
		{"foo/bar", "T", "", "foobar"},
		{"  My Post?#1 ", "T", "", "my-post1"},
		{"%2F..", "Fallback title", "", "2f"},
		{"???", "Fallback title", "", "fallback-title"},
		{"Old_Slug", "T", "Old_Slug", "Old_Slug"}, // unchanged legacy slug is kept
		{"Old_Slug2", "T", "Old_Slug", "old-slug2"},
	} {
		if got := cleanSlug(c.input, c.title, c.current); got != c.want {
			t.Errorf("cleanSlug(%q, %q, %q) = %q, want %q", c.input, c.title, c.current, got, c.want)
		}
	}
}

func TestPostSlugValidation(t *testing.T) {
	setupPublic(t)
	const editPattern = "POST /admin/posts/{id}/edit"
	create := func(fields map[string]string) *httptest.ResponseRecorder {
		return postForm(t, CreatePost, "POST /admin/posts/new", "/admin/posts/new", fields)
	}

	if rec := create(map[string]string{"title": "A", "slug": "foo/bar", "body": "x", "tags": "t1"}); rec.Code != http.StatusFound {
		t.Fatalf("create: status %d", rec.Code)
	}
	a, err := db.GetPostBySlug("foobar")
	if err != nil {
		t.Fatalf("slug foo/bar should be stored as foobar: %v", err)
	}
	if len(a.Tags) != 1 || a.Tags[0] != "t1" {
		t.Errorf("tags = %v, want [t1]", a.Tags)
	}

	// Taken slug on create → 409, nothing created.
	if rec := create(map[string]string{"title": "B", "slug": "foobar", "body": "x"}); rec.Code != http.StatusConflict {
		t.Errorf("create with taken slug: status %d, want 409", rec.Code)
	}
	if posts, _ := db.ListPostsMeta(false); len(posts) != 1 {
		t.Errorf("posts after conflict = %d, want 1", len(posts))
	}

	// Taken slug on update → 409, post unchanged.
	if rec := create(map[string]string{"title": "B", "slug": "b", "body": "x"}); rec.Code != http.StatusFound {
		t.Fatalf("create b: status %d", rec.Code)
	}
	b, _ := db.GetPostBySlug("b")
	bPath := "/admin/posts/" + strconv.Itoa(b.ID) + "/edit"
	if rec := postForm(t, UpdatePost, editPattern, bPath, map[string]string{"title": "B2", "slug": "foobar", "body": "y"}); rec.Code != http.StatusConflict {
		t.Errorf("update to taken slug: status %d, want 409", rec.Code)
	}
	if got, _ := db.GetPostByID(b.ID); got.Slug != "b" || got.Title != "B" {
		t.Errorf("post changed despite conflict: slug %q title %q", got.Slug, got.Title)
	}

	// Empty slug on update keeps a usable one instead of storing "".
	if rec := postForm(t, UpdatePost, editPattern, bPath, map[string]string{"title": "", "slug": "", "body": "y"}); rec.Code != http.StatusFound {
		t.Fatalf("update with empty slug: status %d", rec.Code)
	}
	if got, _ := db.GetPostByID(b.ID); got.Slug != "b" {
		t.Errorf("empty slug: stored %q, want b", got.Slug)
	}
}

func TestSeriesSlugValidation(t *testing.T) {
	setupPublic(t)
	if _, err := db.CreateSeries("Trip", "trip", "", "", "", false, true, false, false); err != nil {
		t.Fatal(err)
	}
	rec := postForm(t, CreateSeries, "POST /admin/series/new", "/admin/series/new", map[string]string{"name": "Other", "slug": "trip"})
	if rec.Code != http.StatusConflict {
		t.Errorf("create series with taken slug: status %d, want 409", rec.Code)
	}
	rec = postForm(t, CreateSeries, "POST /admin/series/new", "/admin/series/new", map[string]string{"name": "Other", "slug": "a/b c"})
	if rec.Code != http.StatusFound {
		t.Fatalf("create series: status %d", rec.Code)
	}
	if _, err := db.GetSeriesBySlug("ab-c"); err != nil {
		t.Errorf("slug a/b c should be stored as ab-c: %v", err)
	}
}

func TestSetupRequiresToken(t *testing.T) {
	setupPublic(t)
	EnsureAdminExists()
	token := currentSetupToken()
	if token == "" {
		t.Fatal("no setup token generated for a site without an admin")
	}
	t.Cleanup(func() { setupToken.Store("") })
	creds := url.Values{"login": {"admin"}, "password": {"password123"}}

	if rec := serve(Setup, "GET /admin/setup", "/admin/setup"); rec.Code != http.StatusForbidden {
		t.Errorf("GET without token: status %d, want 403", rec.Code)
	}
	if rec := serve(Setup, "GET /admin/setup", "/admin/setup?token="+token); rec.Code != 200 || !strings.Contains(rec.Body.String(), `value="`+token+`"`) {
		t.Errorf("GET with token: status %d, form should carry the token", rec.Code)
	}
	if rec := postURLForm(Setup, "POST /admin/setup", "/admin/setup", creds); rec.Code != http.StatusForbidden {
		t.Errorf("POST without token: status %d, want 403", rec.Code)
	}
	if ok, _ := db.UserExists(); ok {
		t.Fatal("admin created without the token")
	}

	creds.Set("token", token)
	if rec := postURLForm(Setup, "POST /admin/setup", "/admin/setup", creds); rec.Code != http.StatusFound {
		t.Fatalf("POST with token: status %d", rec.Code)
	}
	if ok, _ := db.UserExists(); !ok {
		t.Fatal("admin not created")
	}
	if currentSetupToken() != "" {
		t.Error("setup token should be cleared once the admin exists")
	}
	// Setup is closed now, even with the old token.
	creds.Set("login", "second")
	if rec := postURLForm(Setup, "POST /admin/setup", "/admin/setup", creds); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/admin" {
		t.Errorf("second setup: %d → %q, want redirect to /admin", rec.Code, rec.Header().Get("Location"))
	}
}

// A failed DB write must surface as an error, not a redirect that looks
// like success.
func TestSaveErrorsSurface(t *testing.T) {
	setupPublic(t)
	db.DB.Close() // every write fails from here on
	for name, h := range map[string]http.HandlerFunc{
		"settings":      SaveSettings,
		"resume":        SaveResume,
		"delete avatar": DeleteAvatar,
	} {
		rec := postURLForm(h, "POST /x", "/x", url.Values{"site_title": {"T"}, "body": {"b"}})
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s with a broken DB: status %d, want 500", name, rec.Code)
		}
	}
}

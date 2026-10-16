package handlers

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
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

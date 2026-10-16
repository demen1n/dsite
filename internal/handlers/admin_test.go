package handlers

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
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

package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNotFoundPublicPage(t *testing.T) {
	setupPublic(t)
	rec := serve(TagPosts, "GET /tag/{slug}", "/tag/nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{"Страница не найдена", `class="site-header"`, `name="robots" content="noindex"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestNotFoundHTMXIsPlain(t *testing.T) {
	setupPublic(t)
	req := httptest.NewRequest("GET", "/whatever", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	NotFound(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<html") {
		t.Error("HTMX request got a full HTML page")
	}
}

func TestNotFoundAdminLayout(t *testing.T) {
	setupPublic(t)
	token, err := NewSession()
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	// Залогиненный — админский лэйаут.
	req := httptest.NewRequest("GET", "/admin/nope", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	rec := httptest.NewRecorder()
	NotFound(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `class="sidebar"`) {
		t.Error("authed admin 404 not rendered in admin layout")
	}

	// Аноним на /admin/* — публичная страница, без сайдбара админки.
	rec = httptest.NewRecorder()
	NotFound(rec, httptest.NewRequest("GET", "/admin/nope", nil))
	if strings.Contains(rec.Body.String(), `class="sidebar"`) {
		t.Error("anonymous request got admin layout")
	}
}

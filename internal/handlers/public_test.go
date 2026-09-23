package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/demen1n/dsite/internal/db"
)

// setupPublic поднимает in-memory БД и реальные шаблоны с фиксированным
// SITE_URL, чтобы canonical/sitemap были детерминированными.
func setupPublic(t *testing.T) {
	t.Helper()
	if err := db.Init(":memory:"); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	t.Cleanup(func() { db.DB.Close() })
	Init("../../templates", t.TempDir(), "Test", "Desc", "https://example.com", false, false)
}

func createTaggedPost(t *testing.T, slug string, published bool, tags ...string) {
	t.Helper()
	id, err := db.CreatePost(slug, "Title "+slug, "body", "<p>body</p>", "", published, 0)
	if err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	if err := db.SetPostTags(int(id), tags); err != nil {
		t.Fatalf("SetPostTags: %v", err)
	}
}

func serve(h http.HandlerFunc, pattern, target string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc(pattern, h)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", target, nil))
	return rec
}

func TestTagPosts(t *testing.T) {
	setupPublic(t)
	createTaggedPost(t, "in-tag", true, "Комсомольск-на-Амуре")
	createTaggedPost(t, "other", true, "Go")

	slug := url.PathEscape("комсомольск-на-амуре")
	rec := serve(TagPosts, "GET /tag/{slug}", "/tag/"+slug)
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"<title>Комсомольск-на-Амуре — записи блога</title>",
		"<h1>Комсомольск-на-Амуре</h1>",
		`<link rel="canonical" href="https://example.com/tag/` + slug + `">`,
		`<meta property="og:type" content="website">`,
		"Заметки и фотографии с тегом «Комсомольск-на-Амуре».",
		"/post/in-tag",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
	if strings.Contains(body, "/post/other") {
		t.Error("body contains post from another tag")
	}

	if rec := serve(TagPosts, "GET /tag/{slug}", "/tag/nope"); rec.Code != 404 {
		t.Errorf("unknown tag: status = %d, want 404", rec.Code)
	}
}

func TestBlogTagQueryRedirects(t *testing.T) {
	setupPublic(t)

	cases := []struct{ target, want string }{
		// Старые ссылки из постов несли имя тега, а не slug.
		{"/blog?tag=" + url.QueryEscape("комсомольск_на_амуре"), "/tag/" + url.PathEscape("комсомольск-на-амуре")},
		{"/blog?tag=go&page=2", "/tag/go?page=2"},
	}
	for _, tc := range cases {
		rec := serve(Index, "GET /blog", tc.target)
		if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != tc.want {
			t.Errorf("%s: %d → %q, want 301 → %q", tc.target, rec.Code, rec.Header().Get("Location"), tc.want)
		}
	}

	rec := serve(Index, "GET /blog", "/blog?utm_source=x")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `<link rel="canonical" href="https://example.com/blog">`) {
		t.Errorf("/blog: status %d, canonical should drop query params", rec.Code)
	}
}

func TestTagLinksUseSlug(t *testing.T) {
	setupPublic(t)
	createTaggedPost(t, "p", true, "Web Dev")

	rec := serve(Index, "GET /blog", "/blog")
	if n := strings.Count(rec.Body.String(), `href="/tag/web-dev"`); n != 2 { // пилюля фильтра + тег в ленте
		t.Errorf("found %d links to /tag/web-dev, want 2", n)
	}
	rec = serve(ViewPost, "GET /post/{slug}", "/post/p")
	if !strings.Contains(rec.Body.String(), `href="/tag/web-dev"`) {
		t.Error("post page: tag link should point to /tag/web-dev")
	}
}

func TestPostJSONLDKeywords(t *testing.T) {
	setupPublic(t)
	createTaggedPost(t, "p", true, "Комсомольск-на-Амуре", `Say "hi"`)

	rec := serve(ViewPost, "GET /post/{slug}", "/post/p")
	m := regexp.MustCompile(`(?s)<script type="application/ld\+json">(.*?)</script>`).FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatal("no JSON-LD block")
	}
	var ld struct {
		Keywords []string `json:"keywords"`
	}
	if err := json.Unmarshal([]byte(m[1]), &ld); err != nil {
		t.Fatalf("JSON-LD is not valid JSON: %v\n%s", err, m[1])
	}
	if len(ld.Keywords) != 2 || ld.Keywords[0] != "Комсомольск-на-Амуре" || ld.Keywords[1] != `Say "hi"` {
		t.Errorf("keywords = %q", ld.Keywords)
	}
}

func TestSitemapIncludesPublishedTags(t *testing.T) {
	setupPublic(t)
	createTaggedPost(t, "pub", true, "Go")
	createTaggedPost(t, "draft", false, "Draft")

	body := serve(Sitemap, "GET /sitemap.xml", "/sitemap.xml").Body.String()
	if !strings.Contains(body, "<loc>https://example.com/tag/go</loc>") {
		t.Error("sitemap missing /tag/go")
	}
	if strings.Contains(body, "/tag/draft") {
		t.Error("sitemap contains draft-only tag")
	}
}

package handlers

import (
	"dsite/internal/db"
	"encoding/xml"
	"fmt"
	"math"
	"net/http"
	"strconv"
)

// ─────────────────────── Home ───────────────────────

// GET /
func Home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	render(w, "home.html", page("", nil))
}

// ─────────────────────── Blog ───────────────────────

type IndexData struct {
	Posts      []db.Post
	Tags       []db.Tag
	ActiveTag  string
	Page       int
	TotalPages int
	HasPrev    bool
	HasNext    bool
}

// GET /blog
func Index(w http.ResponseWriter, r *http.Request) {
	tag := r.URL.Query().Get("tag")
	pageNum, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if pageNum < 1 {
		pageNum = 1
	}

	posts, total, err := db.ListPostsPaginated(tag, pageNum, db.PostsPerPage)
	if err != nil {
		http.Error(w, "DB error", 500)
		return
	}
	tags, err := db.ListAllTags()
	if err != nil {
		http.Error(w, "DB error", 500)
		return
	}

	totalPages := int(math.Ceil(float64(total) / float64(db.PostsPerPage)))
	if totalPages < 1 {
		totalPages = 1
	}

	data := IndexData{
		Posts:      posts,
		Tags:       tags,
		ActiveTag:  tag,
		Page:       pageNum,
		TotalPages: totalPages,
		HasPrev:    pageNum > 1,
		HasNext:    pageNum < totalPages,
	}
	render(w, "index.html", page("Блог", data))
}

// ─────────────────────── Post ───────────────────────

// GET /post/{slug}
func ViewPost(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	post, err := db.GetPostBySlug(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !post.Published {
		http.NotFound(w, r)
		return
	}
	db.IncrementViews(post.ID)
	render(w, "post.html", page(post.Title, post))
}

// ─────────────────────── Gallery ───────────────────────

type GalleryData struct {
	Photos     []db.Photo
	Categories []db.Category
	ActiveCat  string
}

// GET /gallery
func Gallery(w http.ResponseWriter, r *http.Request) {
	cat := r.URL.Query().Get("cat")
	photos, err := db.ListPhotos(cat)
	if err != nil {
		http.Error(w, "DB error", 500)
		return
	}
	cats, err := db.ListCategories()
	if err != nil {
		http.Error(w, "DB error", 500)
		return
	}
	data := GalleryData{Photos: photos, Categories: cats, ActiveCat: cat}
	render(w, "gallery.html", page("Галерея", data))
}

// GET /gallery/filter  — HTMX: returns only the photo grid fragment
func GalleryFilter(w http.ResponseWriter, r *http.Request) {
	cat := r.URL.Query().Get("cat")
	photos, err := db.ListPhotos(cat)
	if err != nil {
		http.Error(w, "DB error", 500)
		return
	}
	cats, err := db.ListCategories()
	if err != nil {
		http.Error(w, "DB error", 500)
		return
	}
	data := GalleryData{Photos: photos, Categories: cats, ActiveCat: cat}
	renderFragment(w, "gallery.html", "gallery_grid", page("Галерея", data))
}

// ─────────────────────── Resume ───────────────────────

// GET /resume
func Resume(w http.ResponseWriter, r *http.Request) {
	resume, err := db.GetResume()
	if err != nil {
		http.Error(w, "DB error", 500)
		return
	}
	render(w, "resume.html", page("Резюме", resume))
}

// ─────────────────────── RSS/Atom feed ───────────────────────

type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Xmlns   string      `xml:"xmlns,attr"`
	Title   string      `xml:"title"`
	Link    atomLink    `xml:"link"`
	Updated string      `xml:"updated"`
	ID      string      `xml:"id"`
	Entries []atomEntry `xml:"entry"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr,omitempty"`
	Type string `xml:"type,attr,omitempty"`
}

type atomEntry struct {
	Title   string   `xml:"title"`
	Link    atomLink `xml:"link"`
	ID      string   `xml:"id"`
	Updated string   `xml:"updated"`
	Summary string   `xml:"summary"`
	Content atomContent `xml:"content"`
}

type atomContent struct {
	Type    string `xml:"type,attr"`
	Content string `xml:",chardata"`
}

// GET /feed.xml
func Feed(w http.ResponseWriter, r *http.Request) {
	posts, _, err := db.ListPostsPaginated("", 1, 20)
	if err != nil {
		http.Error(w, "DB error", 500)
		return
	}

	scheme := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
		scheme = "http"
	}
	base := fmt.Sprintf("%s://%s", scheme, r.Host)

	feed := atomFeed{
		Xmlns: "http://www.w3.org/2005/Atom",
		Title: siteTitle,
		Link:  atomLink{Href: base + "/feed.xml", Rel: "self"},
		ID:    base + "/",
	}

	if len(posts) > 0 {
		feed.Updated = posts[0].CreatedAt.UTC().Format("2006-01-02T15:04:05Z")
	}

	for _, p := range posts {
		link := base + "/post/" + p.Slug
		feed.Entries = append(feed.Entries, atomEntry{
			Title:   p.Title,
			Link:    atomLink{Href: link},
			ID:      link,
			Updated: p.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			Summary: excerpt(p.BodyHTML),
			Content: atomContent{Type: "html", Content: p.BodyHTML},
		})
	}

	w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
	w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	enc.Encode(feed)
}

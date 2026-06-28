package handlers

import (
	"bytes"
	"dsite/internal/db"
	"encoding/xml"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"strconv"
	"strings"
)

// ─────────────────────── Home ───────────────────────

// GET /
func Home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	pd := page("", nil)
	pd.Canonical = baseURL(r) + "/"
	render(w, "home.html", pd)
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
	pd := page("Блог", data)
	pd.Canonical = baseURL(r) + r.URL.RequestURI()
	render(w, "index.html", pd)
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
	pd := page(post.Title, post)
	pd.OGDescription = excerpt(post.BodyHTML)
	base := baseURL(r)
	pd.Canonical = base + "/post/" + post.Slug
	if post.Cover != "" {
		pd.OGImage = base + "/uploads/" + post.Cover
	}
	render(w, "post.html", pd)
}

// ─────────────────────── Gallery ───────────────────────

type GalleryData struct {
	Photos      []db.Photo
	Categories  []db.Category
	Places      []db.Place
	ActiveCat   string
	ActivePlace string
}

// GET /gallery
func Gallery(w http.ResponseWriter, r *http.Request) {
	cat := r.URL.Query().Get("cat")
	place := r.URL.Query().Get("place")
	photos, err := db.ListPhotos(cat, place)
	if err != nil {
		http.Error(w, "DB error", 500)
		return
	}
	BackfillPhotoDimensions(photos)
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
	data := GalleryData{Photos: photos, Categories: cats, Places: places, ActiveCat: cat, ActivePlace: place}
	pd := page("Галерея", data)
	pd.Canonical = baseURL(r) + r.URL.RequestURI()
	render(w, "gallery.html", pd)
}

// GET /gallery/filter  — HTMX: returns only the photo grid fragment
func GalleryFilter(w http.ResponseWriter, r *http.Request) {
	cat := r.URL.Query().Get("cat")
	place := r.URL.Query().Get("place")
	photos, err := db.ListPhotos(cat, place)
	if err != nil {
		http.Error(w, "DB error", 500)
		return
	}
	BackfillPhotoDimensions(photos)
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
	data := GalleryData{Photos: photos, Categories: cats, Places: places, ActiveCat: cat, ActivePlace: place}
	renderFragment(w, "gallery.html", "gallery_grid", page("Галерея", data))
}

// ─────────────────────── Resume ───────────────────────

// GET /resume
func Resume(w http.ResponseWriter, r *http.Request) {
	if ResumeHidden() {
		http.NotFound(w, r)
		return
	}
	resume, err := db.GetResume()
	if err != nil {
		http.Error(w, "DB error", 500)
		return
	}
	pd := page("Резюме", resume)
	pd.Canonical = baseURL(r) + "/resume"
	render(w, "resume.html", pd)
}

// ─────────────────────── Search ───────────────────────

type SearchData struct {
	Query   string
	Results []db.Post
	Done    bool
}

// GET /search
func Search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	data := SearchData{Query: q}
	if q != "" {
		results, err := db.SearchPosts(q)
		if err != nil {
			http.Error(w, "DB error", 500)
			return
		}
		data.Results = results
		data.Done = true
	}
	render(w, "search.html", page("Поиск", data))
}

// ─────────────────────── Sitemap ───────────────────────

type sitemapURL struct {
	Loc        string `xml:"loc"`
	LastMod    string `xml:"lastmod,omitempty"`
	ChangeFreq string `xml:"changefreq,omitempty"`
	Priority   string `xml:"priority,omitempty"`
}

type sitemapDoc struct {
	XMLName xml.Name     `xml:"urlset"`
	Xmlns   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

// GET /sitemap.xml
func Sitemap(w http.ResponseWriter, r *http.Request) {
	base := baseURL(r)
	sm := sitemapDoc{
		Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs: []sitemapURL{
			{Loc: base + "/", ChangeFreq: "weekly", Priority: "1.0"},
			{Loc: base + "/blog", ChangeFreq: "weekly", Priority: "0.9"},
			{Loc: base + "/gallery", ChangeFreq: "weekly", Priority: "0.8"},
		},
	}
	if !ResumeHidden() {
		sm.URLs = append(sm.URLs, sitemapURL{Loc: base + "/resume", ChangeFreq: "monthly", Priority: "0.5"})
	}
	posts, err := db.ListPosts(true)
	if err == nil {
		for _, p := range posts {
			sm.URLs = append(sm.URLs, sitemapURL{
				Loc:        base + "/post/" + p.Slug,
				LastMod:    p.UpdatedAt.UTC().Format("2006-01-02"),
				ChangeFreq: "monthly",
				Priority:   "0.8",
			})
		}
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	enc.Encode(sm)
}

// GET /robots.txt
func RobotsTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "User-agent: *\nDisallow: /admin/\nSitemap: %s/sitemap.xml\n", baseURL(r))
}

// camera SVG: dark rounded square with a stylised camera shape
var faviconPNGData []byte

// SetFaviconPNG загружает PNG-фавикон, который будет отдаваться по /favicon.png и /favicon.ico.
func SetFaviconPNG(data []byte) { faviconPNGData = data }

// GET /favicon.png
func FaviconPNG(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(faviconPNGData)
}

// faviconCameraSVG — старый фавикон с фотоаппаратом (бэкап)
const faviconCameraSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">
  <rect width="32" height="32" rx="5" fill="#28282e"/>
  <rect x="4" y="12" width="24" height="15" rx="2" fill="#ffffff"/>
  <path d="M12 12V9a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v3" fill="#ffffff"/>
  <circle cx="16" cy="19.5" r="5" fill="#28282e"/>
  <circle cx="16" cy="19.5" r="3.8" fill="#507090"/>
  <circle cx="16" cy="19.5" r="2.3" fill="#28282e"/>
  <circle cx="14.6" cy="18" r="0.9" fill="rgba(255,255,255,0.7)"/>
</svg>`

const faviconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">
  <circle cx="16" cy="16" r="16" fill="#1c1c1c"/>
  <ellipse cx="16" cy="14" rx="5.5" ry="7.5" fill="#e8a800"/>
  <rect x="15.2" y="21.5" width="1.6" height="4.5" rx="0.8" fill="#b07800"/>
  <line x1="16" y1="7.5" x2="16" y2="21.5" stroke="#b07800" stroke-width="0.7" opacity="0.45"/>
</svg>`

// GET /favicon.svg — camera icon (primary, used by modern browsers and Yandex)
func FaviconSVG(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write([]byte(faviconSVG))
}

func icoFillRect(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	for x := x0; x < x1; x++ {
		for y := y0; y < y1; y++ {
			img.Set(x, y, c)
		}
	}
}

func icoFillCircle(img *image.RGBA, cx, cy, r float64, c color.RGBA) {
	for x := int(cx-r) - 1; x <= int(cx+r)+1; x++ {
		for y := int(cy-r) - 1; y <= int(cy+r)+1; y++ {
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			if dx*dx+dy*dy <= r*r {
				img.Set(x, y, c)
			}
		}
	}
}

func icoFillEllipse(img *image.RGBA, cx, cy, rx, ry float64, c color.RGBA) {
	for x := int(cx-rx) - 1; x <= int(cx+rx)+1; x++ {
		for y := int(cy-ry) - 1; y <= int(cy+ry)+1; y++ {
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			if (dx*dx)/(rx*rx)+(dy*dy)/(ry*ry) <= 1.0 {
				img.Set(x, y, c)
			}
		}
	}
}

// drawCameraFavicon — старый фавикон с фотоаппаратом (бэкап)
func drawCameraFavicon(img *image.RGBA) {
	dark := color.RGBA{0x28, 0x28, 0x2e, 0xff}
	white := color.RGBA{0xff, 0xff, 0xff, 0xff}
	ring := color.RGBA{0x50, 0x70, 0x90, 0xff}
	hilight := color.RGBA{0xe0, 0xe0, 0xf4, 0xff}
	icoFillRect(img, 0, 0, 32, 32, dark)
	icoFillRect(img, 4, 12, 28, 27, white)
	icoFillRect(img, 12, 7, 20, 12, white)
	icoFillCircle(img, 16, 19.5, 5.0, dark)
	icoFillCircle(img, 16, 19.5, 3.8, ring)
	icoFillCircle(img, 16, 19.5, 2.3, dark)
	icoFillCircle(img, 14.6, 18.0, 0.9, hilight)
}

// GET /favicon.ico
func Favicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/x-icon")
	w.Header().Set("Cache-Control", "no-cache")

	var pngData []byte
	if len(faviconPNGData) > 0 {
		pngData = faviconPNGData
	} else {
		// fallback: рисуем листик программно
		img := image.NewRGBA(image.Rect(0, 0, 32, 32))
		icoFillCircle(img, 16, 16, 16, color.RGBA{0x1c, 0x1c, 0x1c, 0xff})
		icoFillEllipse(img, 16, 14, 5.5, 7.5, color.RGBA{0xe8, 0xa8, 0x00, 0xff})
		icoFillRect(img, 15, 22, 17, 26, color.RGBA{0xb0, 0x78, 0x00, 0xff})
		var buf bytes.Buffer
		png.Encode(&buf, img)
		pngData = buf.Bytes()
	}

	offset := uint32(22)
	size := uint32(len(pngData))
	ico := []byte{
		0, 0, 1, 0, 1, 0,
		32, 32, 0, 0, 1, 0, 32, 0,
		byte(size), byte(size >> 8), byte(size >> 16), byte(size >> 24),
		byte(offset), byte(offset >> 8), byte(offset >> 16), byte(offset >> 24),
	}
	ico = append(ico, pngData...)
	w.Write(ico)
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

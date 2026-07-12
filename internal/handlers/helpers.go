package handlers

import (
	"bytes"
	"crypto/rand"
	"dsite/internal/db"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

var (
	baseTmpl      *template.Template
	adminBaseTmpl *template.Template
	templatesDir  string
	uploadsDir    string
	siteTitle     string
	siteDesc      string
	siteURL       string // canonical base URL, e.g. https://demenin.ru
	homeAvatar    string // filename of profile photo, served from /uploads/
	secureCookies bool
	trustedProxy  bool
	md            goldmark.Markdown
	funcMap       template.FuncMap
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
	siteURL = strings.TrimRight(siteBaseURL, "/")
	secureCookies = secure
	trustedProxy = trusted

	md = goldmark.New(
		goldmark.WithExtensions(extension.GFM, extension.Table),
		goldmark.WithRendererOptions(html.WithUnsafe()),
	)

	funcMap = template.FuncMap{
		"safeHTML":     func(s string) template.HTML { return template.HTML(s) },
		"fmtDate":      func(t time.Time) string { return t.Format("2 January 2006") },
		"fmtDateShort": func(t time.Time) string { return t.Format("02.01.2006") },
		"relDate":      relDate,
		"excerpt":      excerpt,
		"postWord":     postWord,
		"hasMore":      func(s string) bool { return strings.Contains(s, "<!--more-->") },
		"beforeMore": func(s string) template.HTML {
			if i := strings.Index(s, "<!--more-->"); i >= 0 {
				return template.HTML(s[:i])
			}
			return template.HTML(s)
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
		"add":  func(a, b int) int { return a + b },
		"sub":  func(a, b int) int { return a - b },
		"join": strings.Join,
		"json": func(v any) (template.JS, error) {
			b, err := json.Marshal(v)
			return template.JS(b), err
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

	go func() {
		for range time.Tick(10 * time.Minute) {
			cleanLoginAttempts()
		}
	}()
}

// render рендерит страницу.
// Схема: base.html содержит {{block "content"}} / {{block "admin_content"}}.
// Файлы страниц содержат только {{define "content"}} без вызова базы.
// Мы клонируем базу и парсим файл страницы в клон — {{define}} переопределяет {{block}}.
// Standalone-шаблоны (login, setup) не используют базу, исполняются по имени define.
func render(w http.ResponseWriter, name string, data any) {
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

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err = t.ExecuteTemplate(w, entrypoint, data); err != nil {
		log.Printf("execute %s (%s): %v", name, entrypoint, err)
		http.Error(w, "Internal Server Error", 500)
	}
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

func RenderMD(src string) (string, error) {
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
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
	Canonical     string
	ResumeHidden  bool
}

var socials map[string]string
var resumeHidden bool
var homeBio template.HTML

func LoadSettings() {
	s := db.GetAllSettings()
	if v := s["site_title"]; v != "" {
		siteTitle = v
	}
	if v := s["site_desc"]; v != "" {
		siteDesc = v
	}
	if v := s["home_bio"]; v != "" {
		if rendered, err := RenderMD(v); err == nil {
			h := strings.TrimSpace(rendered)
			if strings.HasPrefix(h, "<p>") && strings.HasSuffix(h, "</p>") {
				h = strings.TrimSpace(h[3 : len(h)-4])
			}
			homeBio = template.HTML(h)
		}
	} else {
		homeBio = ""
	}
	socials = map[string]string{
		"github":    s["social_github"],
		"telegram":  s["social_telegram"],
		"instagram": s["social_instagram"],
		"twitter":   s["social_twitter"],
		"bluesky":   s["social_bluesky"],
		"mastodon":  s["social_mastodon"],
		"vk":        s["social_vk"],
		"linkedin":  s["social_linkedin"],
		"email":     s["social_email"],
	}
	resumeHidden = s["resume_hidden"] == "1"
	homeAvatar = s["home_avatar"]
}

func ResumeHidden() bool { return resumeHidden }

func page(title string, data any) PageData {
	return PageData{Title: title, SiteTitle: siteTitle, SiteDesc: siteDesc, HomeBio: homeBio, HomeAvatar: homeAvatar, Socials: socials, Data: data, ResumeHidden: resumeHidden}
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

func NewSession() string {
	b := make([]byte, 16)
	rand.Read(b)
	token := hex.EncodeToString(b)
	db.CreateSession(token, time.Now().Add(sessionDuration))
	return token
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
	db.DeleteSession(token)
}

// ───── File utils ─────

// webpDimensions returns the pixel dimensions of a WebP file.
// Supports VP8 (lossy), VP8L (lossless), and VP8X (extended) chunks.
// Returns 0,0 on parse failure.
func webpDimensions(data []byte) (int, int) {
	if len(data) < 30 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return 0, 0
	}
	chunk := string(data[12:16])
	switch chunk {
	case "VP8 ":
		// lossy: width/height at bytes 26-29 (14-bit values, LE uint16)
		if len(data) < 30 {
			return 0, 0
		}
		w := int(binary.LittleEndian.Uint16(data[26:28])) & 0x3FFF
		h := int(binary.LittleEndian.Uint16(data[28:30])) & 0x3FFF
		return w, h
	case "VP8L":
		// lossless: bits 0-13 = width-1, bits 14-27 = height-1
		if len(data) < 25 {
			return 0, 0
		}
		bits := binary.LittleEndian.Uint32(data[21:25])
		w := int(bits&0x3FFF) + 1
		h := int((bits>>14)&0x3FFF) + 1
		return w, h
	case "VP8X":
		// extended: 24-bit LE width-1 at bytes 24-26, height-1 at bytes 27-29
		if len(data) < 30 {
			return 0, 0
		}
		w := (int(data[24]) | int(data[25])<<8 | int(data[26])<<16) + 1
		h := (int(data[27]) | int(data[28])<<8 | int(data[29])<<16) + 1
		return w, h
	}
	return 0, 0
}

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
			w, h := webpDimensions(data)
			if w > 0 && h > 0 {
				if err := db.UpdatePhotoDimensions(p.ID, w, h); err != nil {
					log.Printf("backfill dimensions for photo %d: %v", p.ID, err)
				}
			}
		}
	}()
}

func saveUpload(data []byte, ext string) (string, error) {
	b := make([]byte, 8)
	rand.Read(b)
	filename := fmt.Sprintf("%d_%s%s", time.Now().Unix(), hex.EncodeToString(b), ext)
	path := filepath.Join(uploadsDir, filename)
	return filename, os.WriteFile(path, data, 0644)
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

// relDate возвращает относительную дату на русском для свежих записей,
// иначе — абсолютную дату.
func relDate(t time.Time) string {
	days := int(time.Since(t).Hours() / 24)
	switch {
	case days == 0:
		return "сегодня"
	case days == 1:
		return "вчера"
	case days < 7:
		return fmt.Sprintf("%d %s назад", days, dayWord(days))
	case days < 30:
		w := days / 7
		return fmt.Sprintf("%d %s назад", w, weekWord(w))
	default:
		return t.Format("2 Jan 2006")
	}
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

func EnsureAdminExists() {
	exists, err := db.UserExists()
	if err != nil {
		log.Fatal("check user:", err)
	}
	if !exists {
		log.Println("⚠️  No admin user found. Visit /admin/setup to create one.")
	}
}

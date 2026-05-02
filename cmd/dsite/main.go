package main

import (
	"dsite/internal/config"
	"dsite/internal/db"
	"dsite/internal/handlers"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"
)

func main() {
	cfg := config.LoadConfig()

	// Создаём папку для загрузок
	if err := os.MkdirAll(cfg.UploadsDir, 0755); err != nil {
		log.Fatal("mkdir uploads:", err)
	}

	// Инициализация БД
	if err := db.Init(cfg.DBPath); err != nil {
		log.Fatal("db init:", err)
	}

	// Инициализация хендлеров
	db.CleanExpiredSessions()
	go func() {
		for range time.Tick(time.Hour) {
			db.CleanExpiredSessions()
		}
	}()
	db.SeedSettings(map[string]string{
		"site_title": cfg.SiteTitle,
		"site_desc":  cfg.SiteDesc,
	})
	handlers.Init("./templates", cfg.UploadsDir, cfg.SiteTitle, cfg.SiteDesc, cfg.SecureCookies, cfg.TrustedProxy)
	handlers.LoadSettings()
	handlers.EnsureAdminExists()

	mux := http.NewServeMux()

	// ── Статика ──
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))
	mux.Handle("GET /uploads/", immutableCache(http.StripPrefix("/uploads/", http.FileServer(http.Dir(cfg.UploadsDir)))))

	// ── Публичные ──
	mux.HandleFunc("GET /{$}", handlers.Home)
	mux.HandleFunc("GET /blog", handlers.Index)
	mux.HandleFunc("GET /post/{slug}", handlers.ViewPost)
	mux.HandleFunc("GET /gallery", handlers.Gallery)
	mux.HandleFunc("GET /gallery/filter", handlers.GalleryFilter)
	mux.HandleFunc("GET /resume", handlers.Resume)
	mux.HandleFunc("GET /feed.xml", handlers.Feed)
	mux.HandleFunc("GET /search", handlers.Search)

	// ── Auth ──
	mux.HandleFunc("GET /admin/setup", handlers.Setup)
	mux.HandleFunc("POST /admin/setup", handlers.Setup)
	mux.HandleFunc("GET /admin/login", handlers.Login)
	mux.HandleFunc("POST /admin/login", handlers.Login)
	mux.HandleFunc("POST /admin/logout", handlers.Logout)

	// ── Admin (защищено) ──
	mux.HandleFunc("GET /admin", handlers.RequireAuth(handlers.AdminIndex))
	mux.HandleFunc("GET /admin/", handlers.RequireAuth(handlers.AdminIndex))

	mux.HandleFunc("GET /admin/posts/new", handlers.RequireAuth(handlers.NewPost))
	mux.HandleFunc("POST /admin/posts/new", handlers.RequireAuth(handlers.CreatePost))
	mux.HandleFunc("GET /admin/posts/{id}/edit", handlers.RequireAuth(handlers.EditPostForm))
	mux.HandleFunc("POST /admin/posts/{id}/edit", handlers.RequireAuth(handlers.UpdatePost))
	mux.HandleFunc("POST /admin/posts/{id}/delete", handlers.RequireAuth(handlers.DeletePost))
	mux.HandleFunc("POST /admin/posts/preview", handlers.RequireAuth(handlers.PreviewMD))
	mux.HandleFunc("POST /admin/upload", handlers.RequireAuth(handlers.UploadImage))

	mux.HandleFunc("GET /admin/media/picker", handlers.RequireAuth(handlers.MediaPicker))
	mux.HandleFunc("POST /admin/media/to-gallery", handlers.RequireAuth(handlers.AddToGallery))

	mux.HandleFunc("GET /admin/gallery", handlers.RequireAuth(handlers.AdminGallery))
	mux.HandleFunc("POST /admin/gallery/upload", handlers.RequireAuth(handlers.UploadPhoto))
	mux.HandleFunc("POST /admin/gallery/reorder", handlers.RequireAuth(handlers.ReorderPhotos))
	mux.HandleFunc("POST /admin/gallery/{id}/delete", handlers.RequireAuth(handlers.DeletePhoto))
	mux.HandleFunc("POST /admin/gallery/{id}/category", handlers.RequireAuth(handlers.UpdatePhotoCategory))

	mux.HandleFunc("POST /admin/categories/new", handlers.RequireAuth(handlers.CreateCategory))
	mux.HandleFunc("POST /admin/categories/{id}/delete", handlers.RequireAuth(handlers.DeleteCategory))

	mux.HandleFunc("POST /admin/places/new", handlers.RequireAuth(handlers.CreatePlace))
	mux.HandleFunc("POST /admin/places/{id}/delete", handlers.RequireAuth(handlers.DeletePlace))
	mux.HandleFunc("POST /admin/gallery/{id}/place", handlers.RequireAuth(handlers.UpdatePhotoPlace))

	mux.HandleFunc("GET /admin/resume", handlers.RequireAuth(handlers.AdminResume))
	mux.HandleFunc("POST /admin/resume", handlers.RequireAuth(handlers.SaveResume))

	mux.HandleFunc("GET /admin/settings", handlers.RequireAuth(handlers.AdminSettings))
	mux.HandleFunc("POST /admin/settings", handlers.RequireAuth(handlers.SaveSettings))

	mux.HandleFunc("GET /admin/uploads", handlers.RequireAuth(handlers.AdminUploads))
	mux.HandleFunc("POST /admin/uploads/{filename}/delete", handlers.RequireAuth(handlers.DeleteUploadFile))

	log.Printf("🚀 Server running at http://localhost:%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, securityHeaders(csrfCheck(mux))); err != nil {
		log.Fatal(err)
	}
}

// immutableCache sets a 1-year Cache-Control for uploads whose filenames are
// content-addressed random hex strings and never change.
func immutableCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data: blob:; "+
				"connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}

// csrfCheck rejects POST requests from foreign origins.
// Checks Origin header first; falls back to Referer for form submissions
// that don't include Origin.
func csrfCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != r.Host {
					http.Error(w, "Forbidden", http.StatusForbidden)
					return
				}
			} else if referer := r.Header.Get("Referer"); referer != "" {
				u, err := url.Parse(referer)
				if err != nil || u.Host != r.Host {
					http.Error(w, "Forbidden", http.StatusForbidden)
					return
				}
			} else {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

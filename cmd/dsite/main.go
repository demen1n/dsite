package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/demen1n/dsite/internal/backup"
	"github.com/demen1n/dsite/internal/config"
	"github.com/demen1n/dsite/internal/db"
	"github.com/demen1n/dsite/internal/handlers"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	cfg := config.LoadConfig()

	if len(os.Args) > 1 && os.Args[1] == "backup" {
		if err := runBackupOnce(cfg); err != nil {
			log.Fatal("backup: ", err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "restore" {
		if err := runRestoreOnce(cfg, os.Args[2:]); err != nil {
			log.Fatal("restore: ", err)
		}
		return
	}

	if cfg.SiteURL == "" {
		log.Println("WARNING: SITE_URL is not set — sitemap/feed/canonical URLs will be built from the request Host header, which a client can spoof. Set SITE_URL in production.")
	}

	// Создаём папку для загрузок
	if err := os.MkdirAll(cfg.UploadsDir, 0750); err != nil {
		log.Fatal("mkdir uploads:", err)
	}

	// Инициализация БД
	if err := db.Init(cfg.DBPath); err != nil {
		log.Fatal("db init:", err)
	}

	// Инициализация хендлеров
	if err := db.CleanExpiredSessions(); err != nil {
		log.Printf("CleanExpiredSessions: %v", err)
	}
	go func() {
		for range time.Tick(time.Hour) {
			if err := db.CleanExpiredSessions(); err != nil {
				log.Printf("CleanExpiredSessions: %v", err)
			}
		}
	}()
	if cfg.BackupInterval > 0 {
		if remote, err := backup.NewRemote(cfg.BackupRemote, cfg.YaDiskToken, cfg.BackupRemoteDir); err != nil {
			log.Printf("backup: %v — automatic backups disabled", err)
		} else {
			go func() {
				for range time.Tick(cfg.BackupInterval) {
					path, err := backup.Run(context.Background(), backup.Config{
						DB:         db.DB,
						UploadsDir: cfg.UploadsDir,
						OutDir:     cfg.BackupDir,
						Keep:       cfg.BackupKeep,
						Remote:     remote,
						RemoteKeep: cfg.BackupRemoteKeep,
					})
					if err != nil {
						log.Printf("backup: %v", err)
					} else {
						log.Println("backup: completed,", path)
					}
				}
			}()
		}
	}
	db.SeedSettings(map[string]string{
		"site_title": cfg.SiteTitle,
		"site_desc":  cfg.SiteDesc,
		"home_bio":   `Пишу в [блоге](/blog), фотографирую — смотри [галерею](/gallery).`,
	})
	handlers.Init("./templates", cfg.UploadsDir, cfg.SiteTitle, cfg.SiteDesc, cfg.SiteURL, cfg.SecureCookies, cfg.TrustedProxy)
	handlers.LoadSettings()
	if data, err := os.ReadFile("./static/favicon.png"); err == nil {
		handlers.SetFaviconPNG(data)
	}
	handlers.EnsureAdminExists()

	mux := http.NewServeMux()

	// ── Статика ──
	mux.Handle("GET /static/", cacheControl("public, max-age=3600")(noListing(http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))))
	mux.Handle("GET /uploads/", immutableCache(noListing(http.StripPrefix("/uploads/", http.FileServer(http.Dir(cfg.UploadsDir))))))

	// ── Публичные ──
	mux.HandleFunc("GET /{$}", handlers.Home)
	mux.HandleFunc("GET /blog", handlers.Index)
	mux.HandleFunc("GET /post/{slug}", handlers.ViewPost)
	mux.HandleFunc("GET /gallery", handlers.Gallery)
	mux.HandleFunc("GET /gallery/filter", handlers.GalleryFilter)
	mux.HandleFunc("GET /series", handlers.SeriesIndex)
	mux.HandleFunc("GET /series/{slug}", handlers.SeriesView)
	mux.HandleFunc("GET /resume", handlers.Resume)
	mux.HandleFunc("GET /privacy", handlers.Privacy)
	mux.HandleFunc("GET /feed.xml", handlers.Feed)
	mux.HandleFunc("GET /search", handlers.Search)
	mux.HandleFunc("GET /sitemap.xml", handlers.Sitemap)
	mux.HandleFunc("GET /robots.txt", handlers.RobotsTxt)
	mux.HandleFunc("GET /favicon.svg", handlers.FaviconSVG)
	mux.HandleFunc("GET /favicon.png", handlers.FaviconPNG)
	mux.HandleFunc("GET /favicon.ico", handlers.Favicon)

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
	mux.HandleFunc("POST /admin/tags/delete", handlers.RequireAuth(handlers.DeleteTag))

	mux.HandleFunc("GET /admin/series", handlers.RequireAuth(handlers.AdminSeries))
	mux.HandleFunc("GET /admin/series/new", handlers.RequireAuth(handlers.NewSeriesForm))
	mux.HandleFunc("POST /admin/series/new", handlers.RequireAuth(handlers.CreateSeries))
	mux.HandleFunc("GET /admin/series/{id}/edit", handlers.RequireAuth(handlers.EditSeriesForm))
	mux.HandleFunc("POST /admin/series/{id}/edit", handlers.RequireAuth(handlers.UpdateSeries))
	mux.HandleFunc("POST /admin/series/{id}/delete", handlers.RequireAuth(handlers.DeleteSeries))
	mux.HandleFunc("POST /admin/series/reorder", handlers.RequireAuth(handlers.ReorderSeries))

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
	mux.HandleFunc("POST /admin/settings/password", handlers.RequireAuth(handlers.ChangePassword))
	mux.HandleFunc("POST /admin/settings/avatar", handlers.RequireAuth(handlers.UploadAvatar))
	mux.HandleFunc("POST /admin/settings/avatar/delete", handlers.RequireAuth(handlers.DeleteAvatar))

	mux.HandleFunc("GET /admin/uploads", handlers.RequireAuth(handlers.AdminUploads))
	mux.HandleFunc("POST /admin/uploads/{filename}/delete", handlers.RequireAuth(handlers.DeleteUploadFile))

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           securityHeaders(csrfCheck(mux)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("🚀 Server running at http://localhost:%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	stop()
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
	if err := db.DB.Close(); err != nil {
		log.Printf("db close: %v", err)
	}
	log.Println("stopped")
}

// runBackupOnce handles `dsite backup`: a standalone one-shot invocation for
// cron or manual use, independent of the automatic in-process backups the
// server can also run on cfg.BackupInterval.
func runBackupOnce(cfg config.Config) error {
	if err := db.Init(cfg.DBPath); err != nil {
		return fmt.Errorf("db init: %w", err)
	}
	defer db.DB.Close()

	remote, err := backup.NewRemote(cfg.BackupRemote, cfg.YaDiskToken, cfg.BackupRemoteDir)
	if err != nil {
		return err
	}
	path, err := backup.Run(context.Background(), backup.Config{
		DB:         db.DB,
		UploadsDir: cfg.UploadsDir,
		OutDir:     cfg.BackupDir,
		Keep:       cfg.BackupKeep,
		Remote:     remote,
		RemoteKeep: cfg.BackupRemoteKeep,
	})
	if err != nil {
		return err
	}
	log.Println("backup written to", path)
	return nil
}

// runRestoreOnce handles `dsite restore [--force] <archive.tar.gz>`: replaces
// DB_PATH and UPLOADS_DIR with the contents of a backup archive. Destructive,
// so it asks for interactive confirmation unless --force is passed, and
// intentionally doesn't touch db.Init/db.DB — the server (and its DB
// connection) is expected to be stopped before this runs.
func runRestoreOnce(cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	force := fs.Bool("force", false, "skip the confirmation prompt")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: dsite restore [--force] <archive.tar.gz>")
	}
	archivePath := fs.Arg(0)

	if !*force {
		fmt.Printf("This replaces %s and everything in %s with the contents of %s.\n", cfg.DBPath, cfg.UploadsDir, archivePath)
		fmt.Print("Make sure the server is stopped first. Type \"yes\" to continue: ")
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(answer) != "yes" {
			return fmt.Errorf("aborted")
		}
	}

	if err := backup.Restore(backup.RestoreConfig{
		ArchivePath: archivePath,
		DBPath:      cfg.DBPath,
		UploadsDir:  cfg.UploadsDir,
	}); err != nil {
		return err
	}
	log.Println("restore complete — restart the server to pick up the restored data")
	return nil
}

// immutableCache sets a 1-year Cache-Control for uploads whose filenames are
// content-addressed random hex strings and never change.
func immutableCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		next.ServeHTTP(w, r)
	})
}

// cacheControl sets the given Cache-Control value on every response,
// success or not — fine for /static, whose files ship with the binary and
// change only on deploy.
func cacheControl(value string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", value)
			next.ServeHTTP(w, r)
		})
	}
}

// noListing blocks directory listing for file servers.
func noListing(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		// Публичные страницы: только внешние скрипты — inline-XSS в контенте
		// поста не исполнится. Админка (за auth) использует inline-обработчики,
		// поэтому ей оставлен 'unsafe-inline'.
		scriptSrc := "'self'"
		if strings.HasPrefix(r.URL.Path, "/admin") {
			scriptSrc = "'self' 'unsafe-inline'"
		}
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src "+scriptSrc+"; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data: blob:; "+
				"connect-src 'self'; "+
				"base-uri 'none'; "+
				"form-action 'self'; "+
				"frame-ancestors 'self'")
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

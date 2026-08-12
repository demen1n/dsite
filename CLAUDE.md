# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**dsite** (myblog) is a minimalist personal website/blog: posts, photo gallery, resume. Built with Go + HTMX + SQLite. Single binary, zero runtime dependencies.

## Deployment

Push to `main` → GitHub Actions CI/CD автоматически собирает и деплоит на Beget (demenin.ru). Ручной деплой не нужен — просто `git push`. CI (`.github/workflows/ci.yml`) runs gofmt/vet/tests, golangci-lint (config in `.golangci.yml`), and uploads coverage to Codecov on every push and PR.

## Commands

```bash
# Run locally (INSECURE_COOKIES needed for admin login over plain HTTP)
INSECURE_COOKIES=true go run ./cmd/dsite

# Build binary
go build -o dsite ./cmd/dsite

# Format and vet
go fmt ./...
go vet ./...

# Lint (same as CI; config in .golangci.yml)
golangci-lint run ./...

# Docker
docker build -t dsite .
docker run -p 8080:8080 -v dsite_data:/data dsite
```

First run: visit `/admin/setup` to create the admin account.

## Environment Variables

| Variable      | Default          | Description            |
|---------------|------------------|------------------------|
| `PORT`        | `8080`           | Server port            |
| `DB_PATH`     | `./data.db`      | SQLite database path   |
| `UPLOADS_DIR` | `./uploads`      | Photo uploads directory|
| `SITE_TITLE`  | `My Blog`        | Site title             |
| `SITE_DESC`   | `Фото и заметки` | Site subtitle          |
| `SITE_URL`    | _(auto-detect)_  | Canonical base URL (e.g. `https://demenin.ru`), used in sitemap/robots/feeds |
| `INSECURE_COOKIES` | —           | `true` for local development over plain HTTP |
| `TRUSTED_PROXY`    | —           | `true` behind a reverse proxy (trusts `X-Forwarded-For` for login rate limiting) |

## Architecture

```
cmd/dsite/main.go          # Entry point: config, DB init, routing
internal/config/config.go  # Environment config
internal/db/
  db.go                    # SQLite init (WAL mode) + schema migrations
  queries.go               # All database operations (raw SQL)
internal/handlers/
  helpers.go               # Template rendering, sessions, markdown, file uploads
  public.go                # Public routes: /, /post/{slug}, /gallery, /resume
  admin.go                 # Auth + all admin CRUD
templates/
  base.html                # Public layout (CSS embedded here)
  admin/base.html          # Admin layout
  admin/editor.html        # Markdown post editor with HTMX live preview
```

## Key Design Decisions

**Routing**: Uses Go 1.22's `http.ServeMux` with pattern matching (`/post/{slug}`). Auth middleware implemented as a wrapper in admin handlers.

**Templates**: Block inheritance — base template defines layout, page templates override blocks. HTMX fragment responses render standalone without the layout wrapper. The rendering logic is in `helpers.go:renderTemplate`.

**Database**: Raw SQL, no ORM. `modernc.org/sqlite` (pure Go, no CGO). Schema migrations run on startup in `db.go`. Tables: `users`, `posts`, `photos`, `resume`, `categories`, `places`, `series`, `tags`, `post_tags`, `sessions`, `settings`, plus the `posts_fts` FTS5 virtual table (full-text search over post title/body, kept in sync via triggers).

**Sessions**: Stored in SQLite (`sessions` table). 30-day expiry. Expired sessions cleaned on startup and hourly. Single admin user.

**Images**: Server-side resize/re-encode via `internal/imgproc` (EXIF-corrected, max 1600px wide, JPEG output) — replaced the old browser-side canvas resize. GIFs are stored as-is (unprocessed) to preserve animation. Server stores with a random hex filename. Upload validation checks both file extension (allowlist) and magic bytes (MIME content). `imgproc.Process` no longer self-throttles concurrency — callers must hold an `imgproc.Reserve()` slot for the duration of the call (see `internal/imgproc/imgproc.go`).

**Slugs**: `slugify()` in `helpers.go` supports Cyrillic — transliterates to Latin before slugifying, so Russian category/post names work correctly.

**Markdown**: `github.com/yuin/goldmark` with GFM extensions (tables, strikethrough). Unsafe HTML allowed for flexibility. Markdown is stored alongside pre-rendered HTML in the posts table.

**Frontend**: CSS lives in `/static/style.css` (public) and `/static/admin.css` (admin), served with a 1-hour `Cache-Control`. HTMX is vendored and served from `/static/htmx.min.js` (not loaded from a CDN). No build step.

**Security**: `securityHeaders` middleware sets `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, and `Content-Security-Policy`. `csrfCheck` middleware validates `Origin` header on POST requests, falling back to `Referer` for traditional form submissions. Login rate-limited to 5 attempts per 15 min per IP. Set `SITE_URL` in production — if unset, sitemap/feed/canonical URLs fall back to the client-controlled `Host` header (a startup warning is logged when this happens).

**Uploads are never garbage-collected automatically**: replacing a post/series cover or an avatar leaves the old file on disk (deliberate — avoids deleting a file another post/series still references, since usage tracking doesn't cover every place a filename could appear, e.g. series descriptions or the resume body). Clean up manually via `/admin/uploads`, which shows per-file usage and flags unused files.

**Backups**: no automated backup is currently configured. The SQLite file is the only copy of all content, on a single VPS. Recommended: [litestream](https://litestream.io/) streaming WAL to S3-compatible storage, or at minimum a cron job running `sqlite3 /data/data.db ".backup ..."` with rotation.
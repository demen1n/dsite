# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**dsite** (myblog) is a minimalist personal website/blog: posts, photo gallery, resume. Built with Go + HTMX + SQLite. Single binary, zero runtime dependencies.

## Commands

```bash
# Run locally
go run ./cmd/dsite

# Build binary
go build -o dsite ./cmd/dsite

# Format and vet
go fmt ./...
go vet ./...

# Docker
docker build -t myblog .
docker run -p 8080:8080 -v myblog_data:/data myblog
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

**Database**: Raw SQL, no ORM. `modernc.org/sqlite` (pure Go, no CGO). Schema migrations run on startup in `db.go`. Tables: `users`, `posts`, `photos`, `resume`.

**Sessions**: Stored in SQLite (`sessions` table). 30-day expiry. Expired sessions cleaned on startup and hourly. Single admin user.

**Images**: Client-side resize to max 1600px WebP (via OffscreenCanvas in the browser) before upload. Server stores with a random hex filename. Upload validation checks both file extension (allowlist) and magic bytes (MIME content).

**Slugs**: `slugify()` in `helpers.go` supports Cyrillic — transliterates to Latin before slugifying, so Russian category/post names work correctly.

**Markdown**: `github.com/yuin/goldmark` with GFM extensions (tables, strikethrough). Unsafe HTML allowed for flexibility. Markdown is stored alongside pre-rendered HTML in the posts table.

**Frontend**: CSS is embedded directly in `templates/base.html` and `templates/admin/base.html`. HTMX loaded from CDN (`unpkg.com`). No build step.

**Security**: `securityHeaders` middleware sets `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, and `Content-Security-Policy`. `csrfCheck` middleware validates `Origin` header on POST requests, falling back to `Referer` for traditional form submissions. Login rate-limited to 5 attempts per 15 min per IP.
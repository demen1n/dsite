package db

import (
	"database/sql"
	"fmt"
	"log"

	_ "modernc.org/sqlite"
)

var DB *sql.DB

func Init(path string) error {
	var err error
	// modernc.org/sqlite only recognizes pragmas passed via repeated
	// _pragma=name(value) params — _journal_mode=WAL&_foreign_keys=on (the
	// mattn/go-sqlite3 convention) are silently ignored by this driver, so
	// the app had been running without WAL and without FK enforcement
	// (ON DELETE CASCADE/SET NULL in the schema were never actually applied).
	DB, err = sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	if err = DB.Ping(); err != nil {
		return fmt.Errorf("ping db: %w", err)
	}
	if err = migratePhotosOrder(); err != nil {
		return fmt.Errorf("migrate photos order: %w", err)
	}
	if err = migrate(); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if err = migrateCategoryColumn(); err != nil {
		return fmt.Errorf("migrate category column: %w", err)
	}
	if err = migratePostsViews(); err != nil {
		return fmt.Errorf("migrate posts views: %w", err)
	}
	if err = migrateTagsTables(); err != nil {
		return fmt.Errorf("migrate tags tables: %w", err)
	}
	if err = migratePlaces(); err != nil {
		return fmt.Errorf("migrate places: %w", err)
	}
	if err = migrateIndexes(); err != nil {
		return fmt.Errorf("migrate indexes: %w", err)
	}
	if err = migratePhotoDimensions(); err != nil {
		return fmt.Errorf("migrate photo dimensions: %w", err)
	}
	if err = migrateFTS(); err != nil {
		return fmt.Errorf("migrate fts: %w", err)
	}
	if err = migrateSeries(); err != nil {
		return fmt.Errorf("migrate series: %w", err)
	}
	log.Println("DB initialized:", path)
	return nil
}

// migratePhotosOrder adds sort_order column to existing photos tables.
// ALTER TABLE doesn't support IF NOT EXISTS, so we ignore the "duplicate column" error.
func migratePhotosOrder() error {
	DB.Exec(`ALTER TABLE photos ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0`)
	DB.Exec(`UPDATE photos SET sort_order = id WHERE sort_order = 0`)
	return nil
}

// migrateCategoryColumn adds category_id to photos for existing databases.
func migrateCategoryColumn() error {
	DB.Exec(`ALTER TABLE photos ADD COLUMN category_id INTEGER REFERENCES categories(id) ON DELETE SET NULL`)
	return nil
}

func migratePostsViews() error {
	DB.Exec(`ALTER TABLE posts ADD COLUMN views INTEGER NOT NULL DEFAULT 0`)
	return nil
}

func migratePlaces() error {
	_, err := DB.Exec(`
	CREATE TABLE IF NOT EXISTS places (
		id   INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		slug TEXT NOT NULL UNIQUE
	);
	`)
	if err != nil {
		return err
	}
	DB.Exec(`ALTER TABLE photos ADD COLUMN place_id INTEGER REFERENCES places(id) ON DELETE SET NULL`)
	return nil
}

// migrateFTS creates the posts_fts external-content index and the triggers
// that keep it in sync with posts. External-content FTS5 tables can't be
// kept in sync with a plain UPDATE/DELETE on the index — SQLite needs the
// *old* row values fed back through the special 'delete' command before the
// new row goes in, otherwise stale tokens survive edits and deleted rows
// never leave the index. The old triggers (posts_fts_au/posts_fts_ad) did a
// plain UPDATE/DELETE, so they're dropped and recreated here on every
// startup; the unconditional rebuild below heals any index left corrupted
// by the old triggers.
func migrateFTS() error {
	_, err := DB.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS posts_fts USING fts5(
		title, body_md,
		content=posts, content_rowid=id
	)`)
	if err != nil {
		return err
	}
	DB.Exec(`CREATE TRIGGER IF NOT EXISTS posts_fts_ai AFTER INSERT ON posts BEGIN
		INSERT INTO posts_fts(rowid, title, body_md) VALUES (new.id, new.title, new.body_md);
	END`)
	DB.Exec(`DROP TRIGGER IF EXISTS posts_fts_au`)
	DB.Exec(`CREATE TRIGGER posts_fts_au AFTER UPDATE ON posts BEGIN
		INSERT INTO posts_fts(posts_fts, rowid, title, body_md) VALUES('delete', old.id, old.title, old.body_md);
		INSERT INTO posts_fts(rowid, title, body_md) VALUES (new.id, new.title, new.body_md);
	END`)
	DB.Exec(`DROP TRIGGER IF EXISTS posts_fts_ad`)
	DB.Exec(`CREATE TRIGGER posts_fts_ad AFTER DELETE ON posts BEGIN
		INSERT INTO posts_fts(posts_fts, rowid, title, body_md) VALUES('delete', old.id, old.title, old.body_md);
	END`)
	// Heal any index corruption left by the old triggers, and populate the
	// index from existing posts on first run. Idempotent, safe every startup.
	DB.Exec(`INSERT INTO posts_fts(posts_fts) VALUES('rebuild')`)
	return nil
}

// migrateSeries adds the series table (a themed group of posts, e.g. a trip
// spread over several days — "Seoul", "Osaka") and links posts to it.
func migrateSeries() error {
	_, err := DB.Exec(`
	CREATE TABLE IF NOT EXISTS series (
		id               INTEGER PRIMARY KEY AUTOINCREMENT,
		name             TEXT NOT NULL,
		slug             TEXT NOT NULL UNIQUE,
		description_md   TEXT NOT NULL DEFAULT '',
		description_html TEXT NOT NULL DEFAULT '',
		cover            TEXT NOT NULL DEFAULT '',
		collect_photos   INTEGER NOT NULL DEFAULT 0,
		show_cover       INTEGER NOT NULL DEFAULT 1,
		created_at       TEXT NOT NULL DEFAULT (datetime('now'))
	);
	`)
	if err != nil {
		return err
	}
	DB.Exec(`ALTER TABLE posts ADD COLUMN series_id INTEGER REFERENCES series(id) ON DELETE SET NULL`)
	DB.Exec(`ALTER TABLE series ADD COLUMN collect_photos INTEGER NOT NULL DEFAULT 0`)
	DB.Exec(`ALTER TABLE series ADD COLUMN show_cover INTEGER NOT NULL DEFAULT 1`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_posts_series_id ON posts(series_id)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_series_slug ON series(slug)`)
	// sort_order: manual ordering set via admin drag-reorder. Reorder always
	// writes 1-indexed values (see UpdateSeriesOrder), so 0 stays a safe
	// "unset" sentinel for this backfill on every future startup.
	DB.Exec(`ALTER TABLE series ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0`)
	DB.Exec(`UPDATE series SET sort_order = id WHERE sort_order = 0`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_series_sort_order ON series(sort_order)`)
	return nil
}

func migratePhotoDimensions() error {
	DB.Exec(`ALTER TABLE photos ADD COLUMN width INTEGER NOT NULL DEFAULT 0`)
	DB.Exec(`ALTER TABLE photos ADD COLUMN height INTEGER NOT NULL DEFAULT 0`)
	return nil
}

func migrateIndexes() error {
	_, err := DB.Exec(`
	CREATE INDEX IF NOT EXISTS idx_posts_created_at ON posts(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_posts_published ON posts(published);
	CREATE INDEX IF NOT EXISTS idx_photos_sort_order ON photos(sort_order);
	CREATE INDEX IF NOT EXISTS idx_categories_slug ON categories(slug);
	CREATE INDEX IF NOT EXISTS idx_places_slug ON places(slug);
	CREATE INDEX IF NOT EXISTS idx_tags_slug ON tags(slug);
	CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
	`)
	return err
}

func migrateTagsTables() error {
	_, err := DB.Exec(`
	CREATE TABLE IF NOT EXISTS tags (
		id   INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		slug TEXT NOT NULL UNIQUE
	);
	CREATE TABLE IF NOT EXISTS post_tags (
		post_id INTEGER NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
		tag_id  INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
		PRIMARY KEY (post_id, tag_id)
	);
	`)
	return err
}

func migrate() error {
	_, err := DB.Exec(`
	CREATE TABLE IF NOT EXISTS users (
		id       INTEGER PRIMARY KEY,
		login    TEXT NOT NULL UNIQUE,
		password TEXT NOT NULL  -- bcrypt hash
	);

	CREATE TABLE IF NOT EXISTS posts (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		slug       TEXT NOT NULL UNIQUE,
		title      TEXT NOT NULL,
		body_md    TEXT NOT NULL DEFAULT '',  -- исходный Markdown
		body_html  TEXT NOT NULL DEFAULT '',  -- скомпилированный HTML
		cover      TEXT NOT NULL DEFAULT '',  -- путь к обложке
		published  INTEGER NOT NULL DEFAULT 0, -- 0=draft, 1=published
		views      INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS categories (
		id   INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		slug TEXT NOT NULL UNIQUE
	);

	CREATE TABLE IF NOT EXISTS photos (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		filename    TEXT NOT NULL,
		caption     TEXT NOT NULL DEFAULT '',
		sort_order  INTEGER NOT NULL DEFAULT 0,
		category_id INTEGER REFERENCES categories(id) ON DELETE SET NULL,
		created_at  TEXT NOT NULL DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS resume (
		id      INTEGER PRIMARY KEY DEFAULT 1,
		body_md   TEXT NOT NULL DEFAULT '',
		body_html TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS sessions (
		token      TEXT PRIMARY KEY,
		expires_at TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS settings (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL DEFAULT ''
	);

	-- Дефолтная запись резюме
	INSERT OR IGNORE INTO resume (id, body_md, body_html) VALUES (1, '', '');
	`)
	return err
}

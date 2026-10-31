package db

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

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
	if err = migratePostsPublishedAt(); err != nil {
		return fmt.Errorf("migrate posts published_at: %w", err)
	}
	if err = migrateThumbnails(); err != nil {
		return fmt.Errorf("migrate thumbnails: %w", err)
	}
	if err = migrateSlugHistory(); err != nil {
		return fmt.Errorf("migrate slug history: %w", err)
	}
	log.Println("DB initialized:", path)
	return nil
}

// Migrations are idempotent and run on every startup. Every statement's
// error is returned: they used to be ignored wholesale (to swallow ALTER
// TABLE's "duplicate column"), which also hid real failures — a full disk or
// a locked file let the server start on a half-migrated schema. Columns are
// now added through addColumn, which checks first instead of relying on
// the error.

// execAll runs statements in order, stopping at the first error.
func execAll(stmts ...string) error {
	for _, q := range stmts {
		if _, err := DB.Exec(q); err != nil {
			return fmt.Errorf("%s: %w", firstLine(q), err)
		}
	}
	return nil
}

func firstLine(q string) string {
	q = strings.TrimSpace(q)
	if i := strings.IndexByte(q, '\n'); i >= 0 {
		q = q[:i]
	}
	return q
}

// hasColumn reports whether table has column (via PRAGMA table_info).
func hasColumn(table, column string) (bool, error) {
	rows, err := DB.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		found = found || name == column
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return found, nil
}

// addColumn adds column (with its SQL type/constraints in def) to table
// unless it's already there — ALTER TABLE has no ADD COLUMN IF NOT EXISTS.
// table and column are fixed names from this file, never user input.
func addColumn(table, column, def string) error {
	ok, err := hasColumn(table, column)
	if err != nil {
		return fmt.Errorf("table_info %s: %w", table, err)
	}
	if ok {
		return nil
	}
	if _, err := DB.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + def); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, column, err)
	}
	return nil
}

// migratePhotosOrder adds sort_order column to existing photos tables.
func migratePhotosOrder() error {
	// Fresh install: photos doesn't exist yet, migrate() creates it with
	// the column.
	if ok, err := hasTable("photos"); err != nil || !ok {
		return err
	}
	if err := addColumn("photos", "sort_order", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	return execAll(`UPDATE photos SET sort_order = id WHERE sort_order = 0`)
}

func hasTable(name string) (bool, error) {
	var n int
	err := DB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n)
	return n > 0, err
}

// migrateCategoryColumn adds category_id to photos for existing databases.
func migrateCategoryColumn() error {
	return addColumn("photos", "category_id", "INTEGER REFERENCES categories(id) ON DELETE SET NULL")
}

func migratePostsViews() error {
	return addColumn("posts", "views", "INTEGER NOT NULL DEFAULT 0")
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
	return addColumn("photos", "place_id", "INTEGER REFERENCES places(id) ON DELETE SET NULL")
}

// ftsIndexVersion is bumped whenever the posts_fts triggers change in a way
// that leaves an index built by the old ones stale; migrateFTS then rebuilds
// it once (tracked in PRAGMA user_version) instead of on every startup —
// a full rebuild re-tokenizes every post, which grows with the blog.
//
// 1: canonical external-content triggers ('delete' command with the old
// values; the original ones did a plain UPDATE/DELETE and left stale
// tokens), update trigger scoped to title/body_md.
const ftsIndexVersion = 1

// migrateFTS creates the posts_fts external-content index and the triggers
// that keep it in sync with posts. External-content FTS5 tables can't be
// kept in sync with a plain UPDATE/DELETE on the index — SQLite needs the
// *old* row values fed back through the special 'delete' command before the
// new row goes in, otherwise stale tokens survive edits and deleted rows
// never leave the index. The triggers are recreated on every startup (so a
// changed definition here always takes effect), in one transaction with
// the rebuild, so an interrupted startup can't leave posts without them.
func migrateFTS() error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, q := range []string{
		`CREATE VIRTUAL TABLE IF NOT EXISTS posts_fts USING fts5(
			title, body_md,
			content=posts, content_rowid=id
		)`,
		`DROP TRIGGER IF EXISTS posts_fts_ai`,
		`CREATE TRIGGER posts_fts_ai AFTER INSERT ON posts BEGIN
			INSERT INTO posts_fts(rowid, title, body_md) VALUES (new.id, new.title, new.body_md);
		END`,
		// Only on title/body_md changes: an unscoped AFTER UPDATE also fired
		// on every view-counter bump (IncrementViews), re-indexing the whole
		// post on each page view.
		`DROP TRIGGER IF EXISTS posts_fts_au`,
		`CREATE TRIGGER posts_fts_au AFTER UPDATE OF title, body_md ON posts BEGIN
			INSERT INTO posts_fts(posts_fts, rowid, title, body_md) VALUES('delete', old.id, old.title, old.body_md);
			INSERT INTO posts_fts(rowid, title, body_md) VALUES (new.id, new.title, new.body_md);
		END`,
		`DROP TRIGGER IF EXISTS posts_fts_ad`,
		`CREATE TRIGGER posts_fts_ad AFTER DELETE ON posts BEGIN
			INSERT INTO posts_fts(posts_fts, rowid, title, body_md) VALUES('delete', old.id, old.title, old.body_md);
		END`,
	} {
		if _, err := tx.Exec(q); err != nil {
			return fmt.Errorf("%s: %w", firstLine(q), err)
		}
	}

	var version int
	if err := tx.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version < ftsIndexVersion {
		// Heals an index built by older triggers and fills it on first run.
		if _, err := tx.Exec(`INSERT INTO posts_fts(posts_fts) VALUES('rebuild')`); err != nil {
			return fmt.Errorf("rebuild posts_fts: %w", err)
		}
		// PRAGMA can't take a bound parameter; the value is a constant.
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, ftsIndexVersion)); err != nil {
			return err
		}
	}
	return tx.Commit()
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
	for _, c := range [][3]string{
		{"posts", "series_id", "INTEGER REFERENCES series(id) ON DELETE SET NULL"},
		{"series", "collect_photos", "INTEGER NOT NULL DEFAULT 0"},
		{"series", "show_cover", "INTEGER NOT NULL DEFAULT 1"},
		// sort_order: manual ordering set via admin drag-reorder. Reorder
		// always writes 1-indexed values (see UpdateSeriesOrder), so 0 stays
		// a safe "unset" sentinel for the backfill below on every startup.
		{"series", "sort_order", "INTEGER NOT NULL DEFAULT 0"},
		// hidden: серия не показывается в /series и sitemap, а её страница
		// отдаёт 404. Посты серии остаются в ленте как обычные.
		{"series", "hidden", "INTEGER NOT NULL DEFAULT 0"},
		// shuffle_photos: в режиме collect_photos показывать фото в
		// случайном порядке (новом при каждой загрузке), а не по порядку постов.
		{"series", "shuffle_photos", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := addColumn(c[0], c[1], c[2]); err != nil {
			return err
		}
	}
	return execAll(
		`CREATE INDEX IF NOT EXISTS idx_posts_series_id ON posts(series_id)`,
		`CREATE INDEX IF NOT EXISTS idx_series_slug ON series(slug)`,
		`UPDATE series SET sort_order = id WHERE sort_order = 0`,
		`CREATE INDEX IF NOT EXISTS idx_series_sort_order ON series(sort_order)`,
	)
}

// migratePostsPublishedAt adds posts.published_at — когда пост впервые стал
// публичным (или дата, выставленная вручную в редакторе). Лента, серии и
// фиды сортируются по нему, а не по created_at. NULL — пост ещё ни разу не
// публиковался; такие посты сортируются по created_at.
//
// Бэкфилл: уже опубликованным постам ставим published_at = created_at —
// честнее данных у нас нет. Идемпотентно: новые публикации получают
// published_at сразу (см. CreatePost/UpdatePost), так что на следующих
// стартах условию соответствуют только посты из старой базы.
func migratePostsPublishedAt() error {
	if err := addColumn("posts", "published_at", "TEXT"); err != nil {
		return err
	}
	return execAll(`UPDATE posts SET published_at = created_at WHERE published = 1 AND published_at IS NULL`)
}

// migrateThumbnails adds the thumbnails table: превью для любого файла из
// uploads/ (картинки в постах, обложки, фото галереи) и размеры оригинала,
// нужные для srcset. thumb = filename — превью не нужно (GIF или фото не
// шире превью): показываем оригинал.
func migrateThumbnails() error {
	_, err := DB.Exec(`
	CREATE TABLE IF NOT EXISTS thumbnails (
		filename TEXT PRIMARY KEY,
		thumb    TEXT NOT NULL,
		width    INTEGER NOT NULL DEFAULT 0,
		height   INTEGER NOT NULL DEFAULT 0
	)`)
	if err != nil {
		return err
	}
	// placeholder: крошечная копия картинки (data:-URI), которую показываем
	// фоном, пока грузится сама картинка. Пусто — ещё не сгенерирована
	// (BackfillThumbs догенерит) или не нужна (GIF).
	return addColumn("thumbnails", "placeholder", "TEXT NOT NULL DEFAULT ''")
}

// migrateSlugHistory adds slug_history: прежние slug постов и серий, чтобы
// старые ссылки после переименования вели 301-редиректом на новый адрес.
// Ведётся триггерами, так что любой UPDATE slug (из админки или руками в
// базе) попадает в историю. Живой slug всегда важнее истории: при
// переименовании или создании с занятым в истории slug запись удаляется.
func migrateSlugHistory() error {
	_, err := DB.Exec(`
	CREATE TABLE IF NOT EXISTS slug_history (
		kind      TEXT NOT NULL,    -- 'post' | 'series'
		slug      TEXT NOT NULL,    -- прежний slug
		target_id INTEGER NOT NULL, -- id поста/серии
		PRIMARY KEY (kind, slug)
	);
	CREATE TRIGGER IF NOT EXISTS posts_slug_au AFTER UPDATE OF slug ON posts
	WHEN old.slug <> new.slug BEGIN
		INSERT OR REPLACE INTO slug_history (kind, slug, target_id) VALUES ('post', old.slug, old.id);
		DELETE FROM slug_history WHERE kind = 'post' AND slug = new.slug;
	END;
	CREATE TRIGGER IF NOT EXISTS posts_slug_ai AFTER INSERT ON posts BEGIN
		DELETE FROM slug_history WHERE kind = 'post' AND slug = new.slug;
	END;
	CREATE TRIGGER IF NOT EXISTS posts_slug_ad AFTER DELETE ON posts BEGIN
		DELETE FROM slug_history WHERE kind = 'post' AND target_id = old.id;
	END;
	CREATE TRIGGER IF NOT EXISTS series_slug_au AFTER UPDATE OF slug ON series
	WHEN old.slug <> new.slug BEGIN
		INSERT OR REPLACE INTO slug_history (kind, slug, target_id) VALUES ('series', old.slug, old.id);
		DELETE FROM slug_history WHERE kind = 'series' AND slug = new.slug;
	END;
	CREATE TRIGGER IF NOT EXISTS series_slug_ai AFTER INSERT ON series BEGIN
		DELETE FROM slug_history WHERE kind = 'series' AND slug = new.slug;
	END;
	CREATE TRIGGER IF NOT EXISTS series_slug_ad AFTER DELETE ON series BEGIN
		DELETE FROM slug_history WHERE kind = 'series' AND target_id = old.id;
	END;
	`)
	return err
}

func migratePhotoDimensions() error {
	for _, c := range [][3]string{
		{"photos", "width", "INTEGER NOT NULL DEFAULT 0"},
		{"photos", "height", "INTEGER NOT NULL DEFAULT 0"},
		// thumb: превью для сетки галереи, путь относительно uploads/
		// (обычно thumbs/<имя>.jpg; для узких фото и GIF — сам оригинал).
		// Пусто — ещё не сгенерировано: шаблон показывает оригинал,
		// BackfillThumbs догенерит.
		{"photos", "thumb", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := addColumn(c[0], c[1], c[2]); err != nil {
			return err
		}
	}
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

package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// An old database (from before most ALTER TABLE migrations) is brought up
// to date, with the backfills applied, and a second startup is a no-op.
func TestMigrateOldSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE posts (id INTEGER PRIMARY KEY AUTOINCREMENT, slug TEXT NOT NULL UNIQUE, title TEXT NOT NULL,
			body_md TEXT NOT NULL DEFAULT '', body_html TEXT NOT NULL DEFAULT '', cover TEXT NOT NULL DEFAULT '',
			published INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now')))`,
		`CREATE TABLE photos (id INTEGER PRIMARY KEY AUTOINCREMENT, filename TEXT NOT NULL,
			caption TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT (datetime('now')))`,
		`INSERT INTO posts (slug, title, body_md, published, created_at) VALUES ('old', 'Старый пост', 'слово', 1, '2024-01-02 03:04:05')`,
		`INSERT INTO photos (filename) VALUES ('a.jpg'), ('b.jpg')`,
	} {
		if _, err := legacy.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	legacy.Close()

	for run := 1; run <= 2; run++ {
		if err := Init(path); err != nil {
			t.Fatalf("Init run %d: %v", run, err)
		}
		for _, c := range [][2]string{
			{"posts", "views"}, {"posts", "series_id"}, {"posts", "published_at"},
			{"photos", "sort_order"}, {"photos", "category_id"}, {"photos", "place_id"},
			{"photos", "width"}, {"photos", "height"}, {"photos", "thumb"},
			{"series", "hidden"}, {"series", "shuffle_photos"}, {"thumbnails", "placeholder"},
		} {
			if ok, err := hasColumn(c[0], c[1]); err != nil || !ok {
				t.Errorf("run %d: %s.%s missing (err %v)", run, c[0], c[1], err)
			}
		}
		if run < 2 {
			DB.Close()
		}
	}
	t.Cleanup(func() { DB.Close() })

	var sortOrders string
	DB.QueryRow(`SELECT group_concat(sort_order) FROM (SELECT sort_order FROM photos ORDER BY id)`).Scan(&sortOrders) //nolint
	if sortOrders != "1,2" {
		t.Errorf("photos.sort_order backfill = %s, want 1,2", sortOrders)
	}
	p, err := GetPostBySlug("old")
	if err != nil {
		t.Fatal(err)
	}
	if p.PublishedAt.Format("2006-01-02 15:04:05") != "2024-01-02 03:04:05" {
		t.Errorf("published_at backfill = %v", p.PublishedAt)
	}
	if res, _ := SearchPosts("слово"); len(res) != 1 {
		t.Errorf("FTS index not built for the existing post: %d results", len(res))
	}
}

func TestAddColumnErrors(t *testing.T) {
	setupTestDB(t)
	if err := addColumn("no_such_table", "x", "TEXT"); err == nil {
		t.Error("addColumn on a missing table: want error, not a silent no-op")
	}
	if err := addColumn("posts", "views", "INTEGER"); err != nil {
		t.Errorf("addColumn on an existing column should be a no-op: %v", err)
	}
}

// The FTS index is rebuilt once per ftsIndexVersion, not on every startup.
func TestFTSRebuildOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fts.db")
	if err := Init(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { DB.Close() })
	var v int
	DB.QueryRow(`PRAGMA user_version`).Scan(&v) //nolint
	if v != ftsIndexVersion {
		t.Fatalf("user_version = %d, want %d", v, ftsIndexVersion)
	}

	// A stray index entry with no post behind it: a rebuild would drop it.
	ghost := func() int {
		var n int
		DB.QueryRow(`SELECT COUNT(*) FROM posts_fts WHERE posts_fts MATCH 'призрак'`).Scan(&n) //nolint
		return n
	}
	if _, err := DB.Exec(`INSERT INTO posts_fts(rowid, title, body_md) VALUES (999, 'призрак', 'призрак')`); err != nil {
		t.Fatal(err)
	}
	if err := migrateFTS(); err != nil {
		t.Fatal(err)
	}
	if ghost() != 1 {
		t.Error("migrateFTS rebuilt the index although user_version is current")
	}

	DB.Exec(`PRAGMA user_version = 0`) //nolint
	if err := migrateFTS(); err != nil {
		t.Fatal(err)
	}
	if ghost() != 0 {
		t.Error("migrateFTS didn't rebuild the index for an older user_version")
	}
}

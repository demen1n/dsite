package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	dsitedb "github.com/demen1n/dsite/internal/db"
)

// makeTestArchive builds a real backup archive (via Run) from a fresh
// in-memory DB and a small uploads dir, so restore tests exercise the actual
// format Run produces rather than a hand-rolled one.
func makeTestArchive(t *testing.T) string {
	t.Helper()
	setupTestDB(t)

	uploadsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(uploadsDir, "photo.jpg"), []byte("photo bytes"), 0600); err != nil {
		t.Fatal(err)
	}

	archivePath, err := Run(context.Background(), Config{
		DB:         dsitedb.DB,
		UploadsDir: uploadsDir,
		OutDir:     t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return archivePath
}

func TestRestore_RoundTrip(t *testing.T) {
	archivePath := makeTestArchive(t)

	targetDir := t.TempDir()
	dbPath := filepath.Join(targetDir, "data.db")
	uploadsDir := filepath.Join(targetDir, "uploads")

	// stale sidecars from some prior DB at the same path — Restore must
	// clear these or SQLite could try to replay them against the new file.
	if err := os.WriteFile(dbPath+"-wal", []byte("stale wal"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbPath+"-shm", []byte("stale shm"), 0600); err != nil {
		t.Fatal(err)
	}
	// a pre-existing upload not present in the archive — a restore should
	// replace the whole uploads dir, not merge into it.
	if err := os.MkdirAll(uploadsDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(uploadsDir, "stale.jpg"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := Restore(RestoreConfig{ArchivePath: archivePath, DBPath: dbPath, UploadsDir: uploadsDir}); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if _, err := os.Stat(dbPath + "-wal"); !os.IsNotExist(err) {
		t.Error("stale -wal sidecar should have been removed")
	}
	if _, err := os.Stat(dbPath + "-shm"); !os.IsNotExist(err) {
		t.Error("stale -shm sidecar should have been removed")
	}
	if _, err := os.Stat(filepath.Join(uploadsDir, "stale.jpg")); !os.IsNotExist(err) {
		t.Error("upload not present in the archive should have been removed by restore")
	}

	got, err := os.ReadFile(filepath.Join(uploadsDir, "photo.jpg"))
	if err != nil || string(got) != "photo bytes" {
		t.Errorf("restored photo.jpg: got %q, err %v", got, err)
	}

	restoredDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open restored db: %v", err)
	}
	defer func() { _ = restoredDB.Close() }()
	var note string
	if err := restoredDB.QueryRow(`SELECT note FROM canary LIMIT 1`).Scan(&note); err != nil {
		t.Fatalf("query restored db: %v", err)
	}
	if note != "hello backup" {
		t.Errorf("restored canary row: got %q, want %q", note, "hello backup")
	}

	// no leftover staging directories
	if _, err := os.Stat(dbPath + ".restoring"); !os.IsNotExist(err) {
		t.Error("db staging file should have been cleaned up")
	}
	if _, err := os.Stat(uploadsDir + ".restoring"); !os.IsNotExist(err) {
		t.Error("uploads staging dir should have been cleaned up")
	}
}

func TestRestore_MissingDBEntryErrors(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "no-db.tar.gz")
	writeArchiveWithoutDB(t, archivePath)

	target := t.TempDir()
	err := Restore(RestoreConfig{
		ArchivePath: archivePath,
		DBPath:      filepath.Join(target, "data.db"),
		UploadsDir:  filepath.Join(target, "uploads"),
	})
	if err == nil {
		t.Fatal("Restore: want error for an archive with no data.db entry")
	}
}

// writeArchiveWithoutDB hand-builds a tar.gz containing only an uploads/
// entry, to exercise Restore's "archive has no data.db entry" guard —
// something writeArchive (which always adds one) can't produce.
func writeArchiveWithoutDB(t *testing.T, dest string) {
	t.Helper()
	f, err := os.Create(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)
	content := []byte("x")
	if err := tw.WriteHeader(&tar.Header{Name: "uploads/x.jpg", Size: int64(len(content)), Mode: 0600}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
}

package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"

	dsitedb "github.com/demen1n/dsite/internal/db"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	if err := dsitedb.Init(":memory:"); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	t.Cleanup(func() { _ = dsitedb.DB.Close() })
	if _, err := dsitedb.DB.Exec(`CREATE TABLE canary (id INTEGER PRIMARY KEY, note TEXT)`); err != nil {
		t.Fatalf("create canary table: %v", err)
	}
	if _, err := dsitedb.DB.Exec(`INSERT INTO canary (note) VALUES ('hello backup')`); err != nil {
		t.Fatalf("insert canary row: %v", err)
	}
	return dsitedb.DB
}

func readArchive(t *testing.T, path string) map[string][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer func() { _ = f.Close() }()

	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)
	out := map[string][]byte{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("tar read: %v", err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("tar read %s: %v", hdr.Name, err)
		}
		out[hdr.Name] = data
	}
	return out
}

func TestRun_ArchiveContainsDBAndUploads(t *testing.T) {
	setupTestDB(t)

	uploadsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(uploadsDir, "photo.jpg"), []byte("fake jpeg bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(uploadsDir, "sub"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(uploadsDir, "sub", "nested.png"), []byte("nested bytes"), 0600); err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	archivePath, err := Run(context.Background(), Config{
		DB:         dsitedb.DB,
		UploadsDir: uploadsDir,
		OutDir:     outDir,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	entries := readArchive(t, archivePath)

	dbBytes, ok := entries["data.db"]
	if !ok {
		t.Fatal("archive missing data.db")
	}
	if !bytes.HasPrefix(dbBytes, []byte("SQLite format 3\x00")) {
		t.Error("data.db doesn't look like a SQLite file")
	}
	verifySnapshotHasCanaryRow(t, dbBytes)

	if got := string(entries["uploads/photo.jpg"]); got != "fake jpeg bytes" {
		t.Errorf("uploads/photo.jpg: got %q", got)
	}
	if got := string(entries["uploads/sub/nested.png"]); got != "nested bytes" {
		t.Errorf("uploads/sub/nested.png: got %q", got)
	}

	// the temp snapshot file must not leak into OutDir
	if _, err := os.Stat(filepath.Join(outDir, ".snapshot")); err == nil {
		t.Error("snapshot temp file was not cleaned up")
	}
}

// verifySnapshotHasCanaryRow proves VACUUM INTO produced a genuinely queryable
// database, not just a byte-for-byte-plausible file.
func verifySnapshotHasCanaryRow(t *testing.T, dbBytes []byte) {
	t.Helper()
	snapPath := filepath.Join(t.TempDir(), "snapshot.db")
	if err := os.WriteFile(snapPath, dbBytes, 0600); err != nil {
		t.Fatal(err)
	}
	snapDB, err := sql.Open("sqlite", snapPath)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	defer func() { _ = snapDB.Close() }()

	var note string
	if err := snapDB.QueryRow(`SELECT note FROM canary LIMIT 1`).Scan(&note); err != nil {
		t.Fatalf("query snapshot: %v", err)
	}
	if note != "hello backup" {
		t.Errorf("canary row: got %q, want %q", note, "hello backup")
	}
}

func TestRun_MissingUploadsDirIsNotAnError(t *testing.T) {
	setupTestDB(t)

	outDir := t.TempDir()
	archivePath, err := Run(context.Background(), Config{
		DB:         dsitedb.DB,
		UploadsDir: filepath.Join(t.TempDir(), "does-not-exist"),
		OutDir:     outDir,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	entries := readArchive(t, archivePath)
	if _, ok := entries["data.db"]; !ok {
		t.Fatal("archive missing data.db")
	}
	for name := range entries {
		if name != "data.db" {
			t.Errorf("unexpected archive entry %s", name)
		}
	}
}

type fakeRemote struct {
	uploadedPath, uploadedName string
	err                        error
}

func (f *fakeRemote) Upload(_ context.Context, localPath, remoteName string) error {
	f.uploadedPath, f.uploadedName = localPath, remoteName
	return f.err
}

func TestRun_UploadsToRemote(t *testing.T) {
	setupTestDB(t)
	remote := &fakeRemote{}

	archivePath, err := Run(context.Background(), Config{
		DB:         dsitedb.DB,
		UploadsDir: t.TempDir(),
		OutDir:     t.TempDir(),
		Remote:     remote,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if remote.uploadedPath != archivePath {
		t.Errorf("remote got path %q, want %q", remote.uploadedPath, archivePath)
	}
	if remote.uploadedName != filepath.Base(archivePath) {
		t.Errorf("remote got name %q, want %q", remote.uploadedName, filepath.Base(archivePath))
	}
}

func TestRun_RemoteFailureStillReturnsLocalPath(t *testing.T) {
	setupTestDB(t)
	remote := &fakeRemote{err: errors.New("network down")}

	archivePath, err := Run(context.Background(), Config{
		DB:         dsitedb.DB,
		UploadsDir: t.TempDir(),
		OutDir:     t.TempDir(),
		Remote:     remote,
	})
	if err == nil {
		t.Fatal("Run: want error when remote upload fails")
	}
	if archivePath == "" {
		t.Fatal("Run: want local archive path even when remote upload fails")
	}
	if _, statErr := os.Stat(archivePath); statErr != nil {
		t.Errorf("local archive should still exist on disk: %v", statErr)
	}
}

func TestRotate_KeepsOnlyNewest(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		namePrefix + "20260101-000000" + nameSuffix,
		namePrefix + "20260102-000000" + nameSuffix,
		namePrefix + "20260103-000000" + nameSuffix,
		namePrefix + "20260104-000000" + nameSuffix,
		"unrelated.txt",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	rotate(dir, 2)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var remaining []string
	for _, e := range entries {
		remaining = append(remaining, e.Name())
	}
	sort.Strings(remaining)

	want := []string{
		namePrefix + "20260103-000000" + nameSuffix,
		namePrefix + "20260104-000000" + nameSuffix,
		"unrelated.txt",
	}
	sort.Strings(want)
	if len(remaining) != len(want) {
		t.Fatalf("remaining files: got %v, want %v", remaining, want)
	}
	for i := range want {
		if remaining[i] != want[i] {
			t.Errorf("remaining files: got %v, want %v", remaining, want)
			break
		}
	}
}

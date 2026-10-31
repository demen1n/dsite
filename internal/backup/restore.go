package backup

import (
	"archive/tar"
	"compress/gzip"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite" // checkSQLite
)

// RestoreConfig controls where a backup archive gets unpacked to.
type RestoreConfig struct {
	ArchivePath string
	DBPath      string
	UploadsDir  string
}

// Restore replaces the DB file (and uploads dir) at cfg.DBPath/cfg.UploadsDir
// with the contents of the archive at cfg.ArchivePath — the reverse of Run.
//
// This is destructive and assumes nothing else has these paths open: the
// existing DB file and any -wal/-shm sidecars are removed before the
// restored one is installed, and the whole UploadsDir is replaced. Run it
// with the server stopped — swapping the DB file out from under a live WAL
// connection risks corrupting it.
func Restore(cfg RestoreConfig) error {
	f, err := os.Open(cfg.ArchivePath) //nolint:gosec // ArchivePath is an operator-supplied CLI argument, not attacker input
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = gr.Close() }()

	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0750); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(cfg.DBPath), err)
	}

	// Stage both the DB and the uploads dir next to their final paths so a
	// failed or interrupted restore doesn't leave the live paths
	// half-replaced; only rename them into place once extraction succeeds.
	dbTmp := cfg.DBPath + ".restoring"
	defer func() { _ = os.Remove(dbTmp) }()
	uploadsTmp := cfg.UploadsDir + ".restoring"
	if err := os.RemoveAll(uploadsTmp); err != nil {
		return fmt.Errorf("clear staging dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(uploadsTmp) }()

	var sawDB, sawUploads bool
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}
		switch {
		case hdr.Name == "data.db":
			if hdr.Typeflag != tar.TypeReg || sawDB {
				return fmt.Errorf("archive: data.db must be a single regular file")
			}
			if err := extractFile(tr, dbTmp); err != nil {
				return fmt.Errorf("extract data.db: %w", err)
			}
			sawDB = true
		case strings.HasPrefix(hdr.Name, "uploads/") && hdr.Typeflag == tar.TypeReg:
			// Reject "uploads/../x" and friends: anything that would land
			// outside the staging dir. Only regular files and their parent
			// dirs are ever created, so there are no symlinks to escape via.
			rel := filepath.FromSlash(strings.TrimPrefix(hdr.Name, "uploads/"))
			if !filepath.IsLocal(rel) {
				return fmt.Errorf("archive: unsafe path %q", hdr.Name)
			}
			dest := filepath.Join(uploadsTmp, rel)
			if err := os.MkdirAll(filepath.Dir(dest), 0750); err != nil {
				return fmt.Errorf("mkdir %s: %w", filepath.Dir(dest), err)
			}
			if err := extractFile(tr, dest); err != nil {
				return fmt.Errorf("extract %s: %w", hdr.Name, err)
			}
			sawUploads = true
		}
	}
	if !sawDB {
		return fmt.Errorf("archive has no data.db entry")
	}

	if err := checkSQLite(dbTmp); err != nil {
		return fmt.Errorf("archive data.db: %w", err)
	}
	if !sawUploads {
		// Archive predates uploads, or the site simply has none — install a
		// fresh empty dir rather than none at all.
		if err := os.MkdirAll(uploadsTmp, 0750); err != nil {
			return fmt.Errorf("mkdir %s: %w", uploadsTmp, err)
		}
	}
	return install(cfg, dbTmp, uploadsTmp)
}

// install swaps the staged DB and uploads dir into place. The current ones
// (DB plus any -wal/-shm sidecars — stale sidecars must not be replayed
// against the restored file) are first moved aside to "<path>.old" and only
// deleted once both renames succeed; if anything fails midway, whatever was
// already moved is put back, so the live paths end up either fully restored
// or as they were.
func install(cfg RestoreConfig, dbTmp, uploadsTmp string) (err error) {
	type move struct{ from, to string }
	var done []move
	rename := func(from, to string) error {
		if err := os.Rename(from, to); err != nil {
			return err
		}
		done = append(done, move{from, to})
		return nil
	}
	defer func() {
		if err == nil {
			return
		}
		for i := len(done) - 1; i >= 0; i-- {
			if rerr := os.Rename(done[i].to, done[i].from); rerr != nil {
				err = errors.Join(err, fmt.Errorf("rollback %s: %w", done[i].from, rerr))
			}
		}
	}()

	var olds []string
	for _, p := range []string{cfg.DBPath, cfg.DBPath + "-wal", cfg.DBPath + "-shm", cfg.UploadsDir} {
		old := p + ".old"
		if err := os.RemoveAll(old); err != nil {
			return fmt.Errorf("clear %s: %w", old, err)
		}
		if err := rename(p, old); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("move aside %s: %w", p, err)
		}
		olds = append(olds, old)
	}
	if err := rename(dbTmp, cfg.DBPath); err != nil {
		return fmt.Errorf("install restored db: %w", err)
	}
	if err := rename(uploadsTmp, cfg.UploadsDir); err != nil {
		return fmt.Errorf("install restored uploads: %w", err)
	}
	for _, old := range olds {
		_ = os.RemoveAll(old)
	}
	return nil
}

// checkSQLite makes sure path is an intact SQLite database before it
// replaces the live one — a garbage data.db in an otherwise valid archive
// would otherwise silently take the site down.
func checkSQLite(path string) error {
	d, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	var res string
	if err := d.QueryRow(`PRAGMA integrity_check`).Scan(&res); err != nil {
		return err
	}
	if res != "ok" {
		return fmt.Errorf("integrity_check: %s", res)
	}
	return nil
}

func extractFile(r io.Reader, dest string) error {
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(f, r) //nolint:gosec // dest is under our own staging dir; r reads an archive Restore's caller supplied, not attacker input
	return err
}

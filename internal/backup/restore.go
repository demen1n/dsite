package backup

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
			if err := extractFile(tr, dbTmp); err != nil {
				return fmt.Errorf("extract data.db: %w", err)
			}
			sawDB = true
		case strings.HasPrefix(hdr.Name, "uploads/") && hdr.Typeflag == tar.TypeReg:
			rel := strings.TrimPrefix(hdr.Name, "uploads/")
			dest := filepath.Join(uploadsTmp, filepath.FromSlash(rel))
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

	// Clear any stale WAL/SHM sidecars left by whatever DB currently sits at
	// DBPath — otherwise SQLite may try to replay their frames against the
	// freshly-restored file on next open.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		_ = os.Remove(cfg.DBPath + suffix)
	}
	if err := os.Rename(dbTmp, cfg.DBPath); err != nil {
		return fmt.Errorf("install restored db: %w", err)
	}

	if err := os.RemoveAll(cfg.UploadsDir); err != nil {
		return fmt.Errorf("clear uploads dir: %w", err)
	}
	if !sawUploads {
		// Archive predates uploads, or the site simply has none — leave a
		// fresh empty dir rather than none at all.
		return os.MkdirAll(cfg.UploadsDir, 0750)
	}
	return os.Rename(uploadsTmp, cfg.UploadsDir)
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

package backup

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

// writeArchive creates a gzip-compressed tar at dest containing the db
// snapshot (as "data.db") and everything under uploadsDir (as "uploads/...").
//
// gzip.Writer and tar.Writer both buffer and only flush their trailer on
// Close, so — unlike a plain best-effort cleanup Close — a failed Close here
// means a truncated, corrupt archive; every Close error below is checked and
// propagated rather than ignored.
func writeArchive(dest, dbSnapshotPath, uploadsDir string) (err error) {
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()

	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	if err = addFile(tw, dbSnapshotPath, "data.db"); err != nil {
		return fmt.Errorf("add db snapshot: %w", err)
	}
	if err = addDir(tw, uploadsDir, "uploads"); err != nil {
		return fmt.Errorf("add uploads: %w", err)
	}
	if err = tw.Close(); err != nil {
		return fmt.Errorf("finalize tar: %w", err)
	}
	if err = gw.Close(); err != nil {
		return fmt.Errorf("finalize gzip: %w", err)
	}
	return nil
}

func addFile(tw *tar.Writer, srcPath, archiveName string) error {
	// Lstat, not Stat: a symlink must not pull whatever it points at (a file
	// outside uploads, or a huge one) into the archive.
	info, err := os.Lstat(srcPath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s: not a regular file", srcPath)
	}
	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	hdr.Name = archiveName
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }() // read-only handle; nothing left to do with a close error here
	_, err = io.Copy(tw, f)          //nolint:gosec // archive size isn't attacker-controlled; it's our own uploads dir
	return err
}

// addDir walks dir and adds every regular file under archivePrefix in the
// archive, preserving the relative directory structure. Symlinks and other
// non-regular entries are skipped (and logged): uploads only ever contains
// files the server wrote itself, so anything else was put there by hand.
func addDir(tw *tar.Writer, dir, archivePrefix string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil // uploads dir may not exist yet on a fresh install
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			log.Printf("backup: skipping %s: not a regular file", path)
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		return addFile(tw, path, filepath.ToSlash(filepath.Join(archivePrefix, rel)))
	})
}

// Package backup creates point-in-time archives of the SQLite database and
// uploads directory, rotates old local archives, and optionally ships a copy
// to a remote destination (see Remote).
package backup

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const namePrefix = "dsite-backup-"
const nameSuffix = ".tar.gz"

// Config controls what gets backed up, where local archives are written, and
// where an off-site copy (if any) is sent.
type Config struct {
	DB         *sql.DB // open connection to the live database
	UploadsDir string
	OutDir     string
	Keep       int    // local archives to retain; 0 keeps all
	Remote     Remote // optional off-site destination; nil disables it
	RemoteKeep int    // remote archives to retain via Remote.Prune; 0 keeps all
}

// Run snapshots the database, bundles it with the uploads directory into a
// single .tar.gz in cfg.OutDir, rotates old local archives, and — if
// cfg.Remote is set — uploads the new archive there too. Safe to call while
// the server is running: VACUUM INTO takes a snapshot without blocking
// concurrent readers/writers under WAL mode.
func Run(ctx context.Context, cfg Config) (string, error) {
	if err := os.MkdirAll(cfg.OutDir, 0750); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", cfg.OutDir, err)
	}

	ts := time.Now().UTC().Format("20060102-150405")
	snapshotPath := filepath.Join(cfg.OutDir, ".snapshot-"+ts+".db")
	if err := snapshotDB(ctx, cfg.DB, snapshotPath); err != nil {
		return "", fmt.Errorf("snapshot db: %w", err)
	}
	defer os.Remove(snapshotPath)

	archivePath := filepath.Join(cfg.OutDir, namePrefix+ts+nameSuffix)
	if err := writeArchive(archivePath, snapshotPath, cfg.UploadsDir); err != nil {
		return "", fmt.Errorf("write archive: %w", err)
	}

	if cfg.Keep > 0 {
		rotate(cfg.OutDir, cfg.Keep)
	}

	if cfg.Remote != nil {
		if err := cfg.Remote.Upload(ctx, archivePath, filepath.Base(archivePath)); err != nil {
			return archivePath, fmt.Errorf("upload to remote: %w", err)
		}
		if cfg.RemoteKeep > 0 {
			if err := cfg.Remote.Prune(ctx, cfg.RemoteKeep); err != nil {
				log.Printf("backup: remote prune: %v", err)
			}
		}
	}

	return archivePath, nil
}

// snapshotDB takes a consistent copy of the live database via SQLite's
// VACUUM INTO. dest is always a path this package generates itself
// (timestamped, under the configured backup dir) — never external input —
// so it's escaped and inlined rather than bound, since some SQLite driver
// builds don't support parameter binding for VACUUM's target expression.
func snapshotDB(ctx context.Context, db *sql.DB, dest string) error {
	escaped := strings.ReplaceAll(dest, "'", "''")
	//nolint:gosec // G202: dest is a path this package generates itself (see doc comment above), not external input
	_, err := db.ExecContext(ctx, "VACUUM INTO '"+escaped+"'")
	return err
}

// rotate deletes the oldest backups in dir beyond the newest keep, based on
// the sortable timestamp embedded in the filename.
func rotate(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("backup: rotate: %v", err)
		return
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), namePrefix) && strings.HasSuffix(e.Name(), nameSuffix) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // timestamp format sorts lexically = chronologically
	if len(names) <= keep {
		return
	}
	for _, n := range names[:len(names)-keep] {
		if err := os.Remove(filepath.Join(dir, n)); err != nil {
			log.Printf("backup: rotate: remove %s: %v", n, err)
		}
	}
}

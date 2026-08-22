package backup

import (
	"context"
	"fmt"
)

// Remote uploads a completed local backup archive to off-site storage.
type Remote interface {
	Upload(ctx context.Context, localPath, remoteName string) error
	// Prune deletes archives beyond the newest keep on the remote, mirroring
	// the local rotate() behavior. keep <= 0 means keep everything.
	Prune(ctx context.Context, keep int) error
}

// NewRemote builds the Remote named by kind. An empty kind means local-only
// backups and returns (nil, nil). Add new backends' cases here as they're
// implemented (e.g. an S3-compatible one).
func NewRemote(kind, token, dir string) (Remote, error) {
	switch kind {
	case "":
		return nil, nil
	case "yadisk":
		if token == "" {
			return nil, fmt.Errorf("BACKUP_REMOTE=yadisk requires YADISK_TOKEN")
		}
		return YandexDisk{Token: token, Dir: dir}, nil
	default:
		return nil, fmt.Errorf("unknown BACKUP_REMOTE %q", kind)
	}
}

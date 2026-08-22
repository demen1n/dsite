package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

// yaDiskAPI is a var (not a const) so tests can point it at an httptest server.
var yaDiskAPI = "https://cloud-api.yandex.net/v1/disk/resources"

// defaultTimeout bounds the whole request/response cycle of each call
// (connect, write body, read body) — http.DefaultClient has no timeout at
// all, so a stalled connection would otherwise hang Upload forever and, with
// it, the in-process backup ticker in cmd/dsite. Observed real-world uploads
// (~110MB archive over a slow VPS uplink) took ~14 minutes; this leaves
// generous headroom above that while still giving up eventually.
const defaultTimeout = 30 * time.Minute

var defaultHTTPClient = &http.Client{Timeout: defaultTimeout}

// YandexDisk uploads backup archives to a folder in Yandex Disk via its
// REST API: https://yandex.ru/dev/disk/api/reference/upload.html
// Requires an OAuth token with the disk.write (or app_folder) scope,
// obtained from an app registered at https://oauth.yandex.ru.
type YandexDisk struct {
	Token  string
	Dir    string // e.g. "/dsite-backups"
	Client *http.Client
}

// Upload implements Remote.
func (y YandexDisk) Upload(ctx context.Context, localPath, remoteName string) error {
	if err := y.ensureDir(ctx); err != nil {
		return fmt.Errorf("ensure remote dir: %w", err)
	}

	href, err := y.uploadURL(ctx, y.Dir+"/"+remoteName)
	if err != nil {
		return fmt.Errorf("get upload url: %w", err)
	}

	f, err := os.Open(localPath) //nolint:gosec // localPath is always the archive Run just wrote, not external input
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }() // read-only handle; nothing left to do with a close error here
	info, err := f.Stat()
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, href, f)
	if err != nil {
		return err
	}
	req.ContentLength = info.Size()
	resp, err := y.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("yandex disk upload: unexpected status %s", resp.Status)
	}
	return nil
}

// Prune implements Remote. It lists y.Dir, keeps the newest keep archives
// matching this package's backup filename pattern, and moves the rest to
// Yandex Disk's trash (not a permanent delete — a bug here shouldn't be able
// to destroy the only off-site copy of a backup outright).
func (y YandexDisk) Prune(ctx context.Context, keep int) error {
	if keep <= 0 {
		return nil
	}
	names, err := y.list(ctx)
	if err != nil {
		return fmt.Errorf("list %s: %w", y.Dir, err)
	}
	sort.Strings(names) // timestamp-embedded names sort lexically = chronologically
	if len(names) <= keep {
		return nil
	}
	for _, n := range names[:len(names)-keep] {
		if err := y.delete(ctx, y.Dir+"/"+n); err != nil {
			return fmt.Errorf("delete %s: %w", n, err)
		}
	}
	return nil
}

func (y YandexDisk) list(ctx context.Context) ([]string, error) {
	u := yaDiskAPI + "?path=" + url.QueryEscape(y.Dir) + "&fields=_embedded.items.name,_embedded.items.type&limit=1000"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "OAuth "+y.Token)
	resp, err := y.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status %s", resp.Status)
	}
	var body struct {
		Embedded struct {
			Items []struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"items"`
		} `json:"_embedded"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	var names []string
	for _, item := range body.Embedded.Items {
		if item.Type == "file" && strings.HasPrefix(item.Name, namePrefix) && strings.HasSuffix(item.Name, nameSuffix) {
			names = append(names, item.Name)
		}
	}
	return names, nil
}

func (y YandexDisk) delete(ctx context.Context, path string) error {
	u := yaDiskAPI + "?path=" + url.QueryEscape(path)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "OAuth "+y.Token)
	resp, err := y.client().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("unexpected status %s", resp.Status)
	}
	return nil
}

func (y YandexDisk) client() *http.Client {
	if y.Client != nil {
		return y.Client
	}
	return defaultHTTPClient
}

// ensureDir creates y.Dir if it doesn't exist yet; a 409 (already exists) is not an error.
func (y YandexDisk) ensureDir(ctx context.Context) error {
	u := yaDiskAPI + "?path=" + url.QueryEscape(y.Dir)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "OAuth "+y.Token)
	resp, err := y.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusConflict {
		return fmt.Errorf("unexpected status %s", resp.Status)
	}
	return nil
}

func (y YandexDisk) uploadURL(ctx context.Context, remotePath string) (string, error) {
	u := yaDiskAPI + "/upload?path=" + url.QueryEscape(remotePath) + "&overwrite=true"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "OAuth "+y.Token)
	resp, err := y.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("unexpected status %s", resp.Status)
	}
	var body struct {
		Href string `json:"href"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if body.Href == "" {
		return "", fmt.Errorf("empty upload href in response")
	}
	return body.Href, nil
}

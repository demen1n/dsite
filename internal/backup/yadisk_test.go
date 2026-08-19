package backup

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestYandexDisk_Upload(t *testing.T) {
	const token = "test-token"
	var (
		sawEnsureDir, sawUploadURL bool
		sawPUTBody                 []byte
	)

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /v1/disk/resources", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "OAuth "+token {
			t.Errorf("ensureDir auth header: got %q", got)
		}
		sawEnsureDir = true
		w.WriteHeader(http.StatusConflict) // pretend the folder already exists
	})
	mux.HandleFunc("GET /v1/disk/resources/upload", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "OAuth "+token {
			t.Errorf("upload-url auth header: got %q", got)
		}
		if got := r.URL.Query().Get("path"); got != "/dsite-backups/dsite-backup-x.tar.gz" {
			t.Errorf("upload-url path param: got %q", got)
		}
		sawUploadURL = true
		fmt.Fprintf(w, `{"href": "http://%s/uploaded-here", "method": "PUT"}`, r.Host)
	})
	// serves the pre-signed PUT target the fake upload-url endpoint hands back
	mux.HandleFunc("PUT /uploaded-here", func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		if _, err := io.ReadFull(r.Body, body); err != nil {
			t.Errorf("reading PUT body: %v", err)
		}
		sawPUTBody = body
		w.WriteHeader(http.StatusCreated)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	prevAPI := yaDiskAPI
	yaDiskAPI = srv.URL + "/v1/disk/resources"
	t.Cleanup(func() { yaDiskAPI = prevAPI })

	local := filepath.Join(t.TempDir(), "archive.tar.gz")
	if err := os.WriteFile(local, []byte("archive-bytes"), 0600); err != nil {
		t.Fatal(err)
	}

	y := YandexDisk{
		Token:  token,
		Dir:    "/dsite-backups",
		Client: srv.Client(),
	}

	if err := y.Upload(context.Background(), local, "dsite-backup-x.tar.gz"); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if !sawEnsureDir {
		t.Error("ensureDir was never called")
	}
	if !sawUploadURL {
		t.Error("uploadURL was never called")
	}
	if string(sawPUTBody) != "archive-bytes" {
		t.Errorf("uploaded body: got %q, want %q", sawPUTBody, "archive-bytes")
	}
}

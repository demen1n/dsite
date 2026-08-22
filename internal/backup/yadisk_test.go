package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestYandexDisk_client_HasATimeoutByDefault(t *testing.T) {
	y := YandexDisk{Token: "t", Dir: "/d"}
	c := y.client()
	if c.Timeout <= 0 {
		t.Fatal("default client has no timeout — a stalled upload would hang Upload (and the backup ticker) forever")
	}
}

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

func TestYandexDisk_Prune(t *testing.T) {
	const token = "test-token"
	existing := []string{
		namePrefix + "20260101-000000" + nameSuffix,
		namePrefix + "20260102-000000" + nameSuffix,
		namePrefix + "20260103-000000" + nameSuffix,
		namePrefix + "20260104-000000" + nameSuffix,
		"not-a-backup.txt", // must be ignored: doesn't match the naming pattern
	}
	var deleted []string

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/disk/resources", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "OAuth "+token {
			t.Errorf("list auth header: got %q", got)
		}
		type item struct {
			Name string `json:"name"`
			Type string `json:"type"`
		}
		items := make([]item, len(existing))
		for i, n := range existing {
			items[i] = item{Name: n, Type: "file"}
		}
		body := struct {
			Embedded struct {
				Items []item `json:"items"`
			} `json:"_embedded"`
		}{}
		body.Embedded.Items = items
		_ = json.NewEncoder(w).Encode(body)
	})
	mux.HandleFunc("DELETE /v1/disk/resources", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "OAuth "+token {
			t.Errorf("delete auth header: got %q", got)
		}
		if got := r.URL.Query().Get("permanently"); got != "" {
			t.Errorf("delete should not pass permanently=true, got %q", got)
		}
		deleted = append(deleted, r.URL.Query().Get("path"))
		w.WriteHeader(http.StatusNoContent)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	prevAPI := yaDiskAPI
	yaDiskAPI = srv.URL + "/v1/disk/resources"
	t.Cleanup(func() { yaDiskAPI = prevAPI })

	y := YandexDisk{Token: token, Dir: "/dsite-backups", Client: srv.Client()}
	if err := y.Prune(context.Background(), 2); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	sort.Strings(deleted)
	want := []string{
		"/dsite-backups/" + namePrefix + "20260101-000000" + nameSuffix,
		"/dsite-backups/" + namePrefix + "20260102-000000" + nameSuffix,
	}
	if len(deleted) != len(want) {
		t.Fatalf("deleted: got %v, want %v", deleted, want)
	}
	for i := range want {
		if deleted[i] != want[i] {
			t.Errorf("deleted: got %v, want %v", deleted, want)
			break
		}
	}
}

func TestYandexDisk_Prune_KeepsAllWhenUnderLimit(t *testing.T) {
	const token = "test-token"
	deleteCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/disk/resources", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"_embedded": {"items": [{"name": %q, "type": "file"}]}}`, namePrefix+"20260101-000000"+nameSuffix)
	})
	mux.HandleFunc("DELETE /v1/disk/resources", func(w http.ResponseWriter, r *http.Request) {
		deleteCalled = true
		w.WriteHeader(http.StatusNoContent)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	prevAPI := yaDiskAPI
	yaDiskAPI = srv.URL + "/v1/disk/resources"
	t.Cleanup(func() { yaDiskAPI = prevAPI })

	y := YandexDisk{Token: token, Dir: "/dsite-backups", Client: srv.Client()}
	if err := y.Prune(context.Background(), 5); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if deleteCalled {
		t.Error("Prune should not delete anything when the count is under keep")
	}
}

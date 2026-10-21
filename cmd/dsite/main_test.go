package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestImmutableCache_OnlyOnSuccess(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.jpg"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	h := immutableCache(http.StripPrefix("/uploads/", http.FileServer(http.Dir(dir))))

	cases := []struct {
		path      string
		wantCode  int
		wantCache bool
	}{
		{"/uploads/a.jpg", http.StatusOK, true},
		{"/uploads/missing.jpg", http.StatusNotFound, false},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != tc.wantCode {
			t.Errorf("%s: status %d, want %d", tc.path, rec.Code, tc.wantCode)
		}
		if got := rec.Header().Get("Cache-Control") != ""; got != tc.wantCache {
			t.Errorf("%s: Cache-Control set = %v (%q), want %v", tc.path, got, rec.Header().Get("Cache-Control"), tc.wantCache)
		}
	}
}

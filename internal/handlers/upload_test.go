package handlers

import (
	"bytes"
	"image"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/demen1n/dsite/internal/db"
)

type uploadFile struct {
	name string
	data []byte
}

// postFiles шлёт multipart-форму с файлами в поле field прямо в хендлер.
func postFiles(t *testing.T, h http.HandlerFunc, target, field string, files ...uploadFile) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, f := range files {
		fw, err := mw.CreateFormFile(field, f.name)
		if err != nil {
			t.Fatalf("CreateFormFile: %v", err)
		}
		fw.Write(f.data)
	}
	mw.Close()
	req := httptest.NewRequest("POST", target, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func smallJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, 64, 48)), nil); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

// heavyJPEG — только заголовок (SOF2 + SOS без данных) прогрессивного
// 8064x4536 4:4:4 JPEG: по пикселям в норме, но image/jpeg понадобилось бы
// ~550MB, так что imgproc отвергает его ещё до декодирования.
func heavyJPEG() []byte {
	const w, h = 8064, 4536
	return []byte{0xFF, 0xD8, 0xFF, 0xC2, 0, 17, 8, h >> 8, h & 0xFF, w >> 8, w & 0xFF, 3,
		1, 0x11, 0, 2, 0x11, 1, 3, 0x11, 1,
		0xFF, 0xDA, 0, 12, 3, 1, 0, 2, 0, 3, 0, 0, 63, 0,
		0xFF, 0xD9}
}

func TestUploadImageTooLarge(t *testing.T) {
	setupPublic(t)

	rec := postFiles(t, UploadImage, "/admin/upload", "image", uploadFile{"big.jpg", heavyJPEG()})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("heavy: status %d, want 413 (body %q)", rec.Code, rec.Body.String())
	}

	rec = postFiles(t, UploadImage, "/admin/upload", "image", uploadFile{"ok.jpg", smallJPEG(t)})
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), "/uploads/") {
		t.Fatalf("small: status %d body %q, want 200 /uploads/…", rec.Code, rec.Body.String())
	}
}

func TestUploadPhotoReportsTooLargeAndFailed(t *testing.T) {
	setupPublic(t)

	rec := postFiles(t, UploadPhoto, "/admin/gallery/upload", "photos",
		uploadFile{"ok.jpg", smallJPEG(t)},
		uploadFile{"big.jpg", heavyJPEG()},
		uploadFile{"junk.jpg", []byte("not an image at all")},
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("X-Too-Large"); got != "1" {
		t.Errorf("X-Too-Large = %q, want %q", got, "1")
	}
	if got := rec.Header().Get("X-Upload-Failed"); got != "1" {
		t.Errorf("X-Upload-Failed = %q, want %q", got, "1")
	}
	photos, err := db.ListPhotos("", "")
	if err != nil {
		t.Fatalf("ListPhotos: %v", err)
	}
	if len(photos) != 1 {
		t.Errorf("stored %d photos, want 1", len(photos))
	}

	// Всё загрузилось — заголовков нет.
	rec = postFiles(t, UploadPhoto, "/admin/gallery/upload", "photos", uploadFile{"ok.jpg", smallJPEG(t)})
	if rec.Header().Get("X-Too-Large") != "" || rec.Header().Get("X-Upload-Failed") != "" {
		t.Errorf("unexpected headers on clean upload: %v", rec.Header())
	}
}

func TestUploadAvatarTooLarge(t *testing.T) {
	setupPublic(t)

	rec := postFiles(t, UploadAvatar, "/admin/settings/avatar", "avatar", uploadFile{"big.jpg", heavyJPEG()})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "слишком тяжёлое") {
		t.Fatalf("status %d body %q, want 400 with an explanation", rec.Code, rec.Body.String())
	}
}

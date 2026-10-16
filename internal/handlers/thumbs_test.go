package handlers

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/demen1n/dsite/internal/db"
	"github.com/demen1n/dsite/internal/imgproc"
)

func testJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x += 7 {
		img.Set(x, x%h, color.RGBA{R: uint8(x), G: 100, B: 200, A: 255})
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	return buf.Bytes()
}

func TestMakeThumb(t *testing.T) {
	setupPublic(t)
	release := imgproc.Reserve()
	defer release()

	// Широкое фото — превью 800px в thumbs/.
	thumb, err := makeThumb("wide.jpg", testJPEG(t, 1600, 1000), 1600)
	if err != nil || thumb != "thumbs/wide.jpg" {
		t.Fatalf("wide: thumb=%q err=%v", thumb, err)
	}
	data, err := os.ReadFile(filepath.Join(uploadsDir, "thumbs", "wide.jpg"))
	if err != nil {
		t.Fatalf("read thumb: %v", err)
	}
	if w, h, _ := imgproc.Dimensions(data); w != thumbWidth || h != 500 {
		t.Errorf("thumb size = %dx%d, want %dx500", w, h, thumbWidth)
	}

	// Ширина неизвестна (0) — определяется по данным; webp-оригинал даёт .jpg превью.
	if thumb, _ := makeThumb("old.webp", testJPEG(t, 1200, 800), 0); thumb != "thumbs/old.jpg" {
		t.Errorf("unknown width: thumb=%q, want thumbs/old.jpg", thumb)
	}

	// Узкое фото и GIF — превью не нужно, оригинал сам себе превью.
	if thumb, _ := makeThumb("narrow.jpg", testJPEG(t, 640, 480), 640); thumb != "narrow.jpg" {
		t.Errorf("narrow: thumb=%q, want original", thumb)
	}
	var gifBuf bytes.Buffer
	gif.Encode(&gifBuf, image.NewPaletted(image.Rect(0, 0, 1600, 900), color.Palette{color.Black, color.White}), nil) //nolint
	if thumb, _ := makeThumb("anim.gif", gifBuf.Bytes(), 1600); thumb != "anim.gif" {
		t.Errorf("gif: thumb=%q, want original", thumb)
	}
}

func TestBackfillThumbsAndGallery(t *testing.T) {
	setupPublic(t)
	if err := os.WriteFile(filepath.Join(uploadsDir, "big.jpg"), testJPEG(t, 1600, 1067), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(uploadsDir, "small.jpg"), testJPEG(t, 600, 400), 0o600); err != nil {
		t.Fatal(err)
	}
	db.AddPhoto("big.jpg", "", 0, 0, 1600, 1067)  //nolint
	db.AddPhoto("small.jpg", "", 0, 0, 600, 400)  //nolint
	db.AddPhoto("missing.jpg", "", 0, 0, 1600, 1) //nolint — файла нет: пропускаем, не падаем

	BackfillThumbs()

	photos, _ := db.ListPhotos("", "")
	got := map[string]string{}
	for _, p := range photos {
		got[p.Filename] = p.Thumb
	}
	if got["big.jpg"] != "thumbs/big.jpg" || got["small.jpg"] != "small.jpg" || got["missing.jpg"] != "" {
		t.Errorf("thumbs after backfill: %v", got)
	}

	body := serve(Gallery, "GET /gallery", "/gallery").Body.String()
	for _, want := range []string{
		`src="/uploads/thumbs/big.jpg"`,
		`data-srcset="/uploads/thumbs/big.jpg 800w, /uploads/big.jpg 1600w"`,
		`data-full="/uploads/big.jpg"`,
		`src="/uploads/missing.jpg"`, // без превью — оригинал
		`loading="lazy"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("gallery missing %q", want)
		}
	}
	// У узкого фото превью = оригинал, srcset не нужен.
	if strings.Contains(body, `/uploads/small.jpg 600w`) {
		t.Error("narrow photo got a srcset")
	}
}

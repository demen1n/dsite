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

// writeUpload кладёт картинку в uploads/ тестового окружения.
func writeUpload(t *testing.T, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(uploadsDir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMakeThumb(t *testing.T) {
	setupPublic(t)
	release := imgproc.Reserve()
	defer release()

	// Широкое фото — превью 800px в thumbs/, размеры оригинала запомнены.
	th, err := makeThumb("wide.jpg", testJPEG(t, 1600, 1000))
	if err != nil || th.Thumb != "thumbs/wide.jpg" || th.Width != 1600 || th.Height != 1000 {
		t.Fatalf("wide: %+v, err=%v", th, err)
	}
	data, err := os.ReadFile(filepath.Join(uploadsDir, "thumbs", "wide.jpg"))
	if err != nil {
		t.Fatalf("read thumb: %v", err)
	}
	if w, h, _ := imgproc.Dimensions(data); w != thumbWidth || h != 500 {
		t.Errorf("thumb size = %dx%d, want %dx500", w, h, thumbWidth)
	}

	// webp-оригинал даёт .jpg превью.
	if th, _ := makeThumb("old.webp", testJPEG(t, 1200, 800)); th.Thumb != "thumbs/old.jpg" {
		t.Errorf("webp original: thumb=%q, want thumbs/old.jpg", th.Thumb)
	}

	// Узкое фото и GIF — превью не нужно, оригинал сам себе превью.
	if th, _ := makeThumb("narrow.jpg", testJPEG(t, 640, 480)); th.Thumb != "narrow.jpg" || th.Width != 640 {
		t.Errorf("narrow: %+v, want original", th)
	}
	var gifBuf bytes.Buffer
	gif.Encode(&gifBuf, image.NewPaletted(image.Rect(0, 0, 1600, 900), color.Palette{color.Black, color.White}), nil) //nolint
	if th, _ := makeThumb("anim.gif", gifBuf.Bytes()); th.Thumb != "anim.gif" {
		t.Errorf("gif: thumb=%q, want original", th.Thumb)
	}
}

func TestBackfillThumbsAndForget(t *testing.T) {
	setupPublic(t)
	writeUpload(t, "big.jpg", testJPEG(t, 1600, 1067))
	writeUpload(t, "small.jpg", testJPEG(t, 600, 400))
	writeUpload(t, "notes.txt", []byte("not an image"))
	db.AddPhoto("big.jpg", "", 0, 0, 1600, 1067) //nolint

	BackfillThumbs()

	if th, ok := thumbOf("big.jpg"); !ok || th.Thumb != "thumbs/big.jpg" {
		t.Errorf("big.jpg: %+v, %v", th, ok)
	}
	if th, ok := thumbOf("small.jpg"); !ok || th.Thumb != "small.jpg" {
		t.Errorf("small.jpg: %+v, %v", th, ok)
	}
	if _, ok := thumbOf("notes.txt"); ok {
		t.Error("non-image got a thumbnail")
	}
	// Фото галереи получило превью; индекс переживает перезагрузку из БД.
	if photos, _ := db.ListPhotos("", ""); photos[0].Thumb != "thumbs/big.jpg" {
		t.Errorf("photos.thumb = %q", photos[0].Thumb)
	}
	loadThumbIndex()
	if _, ok := thumbOf("big.jpg"); !ok {
		t.Error("thumbnail lost after reload from DB")
	}

	// Удалили файл превью руками — повторный backfill его восстановит.
	os.Remove(filepath.Join(uploadsDir, "thumbs", "big.jpg")) //nolint
	BackfillThumbs()
	if _, err := os.Stat(filepath.Join(uploadsDir, "thumbs", "big.jpg")); err != nil {
		t.Errorf("thumb file not regenerated: %v", err)
	}

	forgetThumb("big.jpg")
	if _, ok := thumbOf("big.jpg"); ok {
		t.Error("forgetThumb: still in index")
	}
	if _, err := os.Stat(filepath.Join(uploadsDir, "thumbs", "big.jpg")); !os.IsNotExist(err) {
		t.Errorf("forgetThumb: thumb file still there (%v)", err)
	}
}

func TestThumbRenderHelpers(t *testing.T) {
	setupPublic(t)
	rememberThumb(db.Thumbnail{Filename: "big.jpg", Thumb: "thumbs/big.jpg", Width: 1600, Height: 1000})
	rememberThumb(db.Thumbnail{Filename: "small.jpg", Thumb: "small.jpg", Width: 600, Height: 400})

	for _, src := range []string{"big.jpg", "/uploads/big.jpg"} {
		if got := thumbSrcset(src); got != "/uploads/thumbs/big.jpg 800w, /uploads/big.jpg 1600w" {
			t.Errorf("thumbSrcset(%q) = %q", src, got)
		}
		if got := thumbSrc(src); got != "/uploads/thumbs/big.jpg" {
			t.Errorf("thumbSrc(%q) = %q", src, got)
		}
	}
	if got := thumbRatio("/uploads/big.jpg"); got != "1600/1000" {
		t.Errorf("thumbRatio = %q", got)
	}
	// Без превью / не наш файл — srcset пустой, src как был.
	for _, src := range []string{"small.jpg", "unknown.jpg", "https://example.com/x.jpg", "/uploads/sub/x.jpg"} {
		if got := thumbSrcset(src); got != "" {
			t.Errorf("thumbSrcset(%q) = %q, want empty", src, got)
		}
	}
	if got := thumbSrc("https://example.com/x.jpg"); got != "https://example.com/x.jpg" {
		t.Errorf("external thumbSrc = %q", got)
	}

	html := `<p><img src="/uploads/big.jpg" alt="a"></p>` +
		`<p><img src="/uploads/small.jpg" alt="b" /></p>` +
		`<p><img src="https://example.com/x.jpg" alt="c"></p>` +
		`<p><img src="/uploads/big.jpg" loading="eager" srcset="custom.jpg 1x" alt="d"></p>`
	got := responsiveImages(html)
	for _, want := range []string{
		`<img src="/uploads/big.jpg" alt="a" srcset="/uploads/thumbs/big.jpg 800w, /uploads/big.jpg 1600w" sizes="` + proseImgSizes + `" width="1600" height="1000" loading="lazy" decoding="async">`,
		`<img src="/uploads/small.jpg" alt="b" width="600" height="400" loading="lazy" decoding="async"/>`,
		`<img src="https://example.com/x.jpg" alt="c" loading="lazy" decoding="async">`,
		// заданное в посте не трогаем
		`<img src="/uploads/big.jpg" loading="eager" srcset="custom.jpg 1x" alt="d" width="1600" height="1000" decoding="async">`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("responsiveImages missing\n  %s\nin\n  %s", want, got)
		}
	}
}

func TestPublicPagesUseThumbs(t *testing.T) {
	setupPublic(t)
	rememberThumb(db.Thumbnail{Filename: "body.jpg", Thumb: "thumbs/body.jpg", Width: 1600, Height: 1000})
	rememberThumb(db.Thumbnail{Filename: "cover.jpg", Thumb: "thumbs/cover.jpg", Width: 1600, Height: 900})
	sid, _ := db.CreateSeries("Trip", "trip", "", "", "cover.jpg", true, true, false, false)
	db.CreatePost("p", "P", "", `<p><img src="/uploads/body.jpg" alt=""></p>`, "cover.jpg", true, int(sid)) //nolint
	cards, _ := db.CreateSeries("Cards", "cards", "", "", "", false, true, false, false)
	db.CreatePost("q", "Q", "", `<p><img src="/uploads/body.jpg" alt=""></p>`, "", true, int(cards)) //nolint

	checks := []struct {
		name, body string
		want       []string
	}{
		{"post", serve(ViewPost, "GET /post/{slug}", "/post/p").Body.String(), []string{
			`srcset="/uploads/thumbs/cover.jpg 800w, /uploads/cover.jpg 1600w"`, // обложка
			`srcset="/uploads/thumbs/body.jpg 800w, /uploads/body.jpg 1600w"`,   // картинка в тексте
			`loading="lazy"`,
		}},
		{"blog", serve(Index, "GET /blog", "/blog").Body.String(), []string{
			`srcset="/uploads/thumbs/cover.jpg 800w`,
			`srcset="/uploads/thumbs/body.jpg 800w`,
		}},
		{"series photos", serve(SeriesView, "GET /series/{slug}", "/series/trip").Body.String(), []string{
			`data-ratio="1600/1000"`,
			`src="/uploads/thumbs/body.jpg"`,
			`data-srcset="/uploads/thumbs/body.jpg 800w, /uploads/body.jpg 1600w"`,
			`data-full="/uploads/body.jpg"`,
		}},
		{"series cards", serve(SeriesView, "GET /series/{slug}", "/series/cards").Body.String(), []string{
			`class="series-card-cover" src="/uploads/thumbs/body.jpg" data-srcset=`,
		}},
		{"series index", serve(SeriesIndex, "GET /series", "/series").Body.String(), []string{
			`srcset="/uploads/thumbs/cover.jpg 800w`,
		}},
	}
	for _, c := range checks {
		for _, want := range c.want {
			if !strings.Contains(c.body, want) {
				t.Errorf("%s: missing %q", c.name, want)
			}
		}
	}
}

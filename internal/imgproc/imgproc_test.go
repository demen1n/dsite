package imgproc

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func makeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 100, A: 255})
		}
	}
	buf := new(bytes.Buffer)
	if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	return buf.Bytes()
}

func TestProcessDownscales(t *testing.T) {
	data := makeJPEG(t, 3200, 2000)
	out, w, h, err := Process(data, 1600, 85)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if w != 1600 || h != 1000 {
		t.Errorf("got %dx%d, want 1600x1000", w, h)
	}
	if _, _, err := image.Decode(bytes.NewReader(out)); err != nil {
		t.Errorf("output isn't valid image: %v", err)
	}
}

func TestProcessNeverUpscales(t *testing.T) {
	data := makeJPEG(t, 800, 600)
	_, w, h, err := Process(data, 1600, 85)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if w != 800 || h != 600 {
		t.Errorf("got %dx%d, want unchanged 800x600", w, h)
	}
}

func TestApplyOrientationSwapsDimensions(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 100, 50))
	for _, o := range []int{5, 6, 7, 8} {
		out := applyOrientation(src, o)
		b := out.Bounds()
		if b.Dx() != 50 || b.Dy() != 100 {
			t.Errorf("orientation %d: got %dx%d, want 50x100", o, b.Dx(), b.Dy())
		}
	}
	for _, o := range []int{1, 2, 3, 4} {
		out := applyOrientation(src, o)
		b := out.Bounds()
		if b.Dx() != 100 || b.Dy() != 50 {
			t.Errorf("orientation %d: got %dx%d, want 100x50", o, b.Dx(), b.Dy())
		}
	}
}

func TestApplyOrientationRotate90CW(t *testing.T) {
	// 2x1 image: (0,0)=red, (1,0)=blue.
	src := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	src.Set(0, 0, color.NRGBA{R: 255, A: 255})
	src.Set(1, 0, color.NRGBA{B: 255, A: 255})

	out := applyOrientation(src, 6) // rotate 90 CW -> 1x2, red on top
	b := out.Bounds()
	if b.Dx() != 1 || b.Dy() != 2 {
		t.Fatalf("got %dx%d, want 1x2", b.Dx(), b.Dy())
	}
	r, _, _, _ := out.At(0, 0).RGBA()
	if r>>8 != 255 {
		t.Errorf("expected red at top after 90 CW rotate, got %v", out.At(0, 0))
	}
	_, _, bl, _ := out.At(0, 1).RGBA()
	if bl>>8 != 255 {
		t.Errorf("expected blue at bottom after 90 CW rotate, got %v", out.At(0, 1))
	}
}

func TestJpegOrientationParsesTag(t *testing.T) {
	if got := jpegOrientation([]byte{0x00, 0x01}); got != 1 {
		t.Errorf("non-JPEG input: got %d, want 1 (no-op)", got)
	}
}

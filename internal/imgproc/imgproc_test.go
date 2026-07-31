package imgproc

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
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
	defer Reserve()()
	data := makeJPEG(t, 3200, 2000)
	out, w, h, err := Process(data, 1600, 0, 85)
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
	defer Reserve()()
	data := makeJPEG(t, 800, 600)
	_, w, h, err := Process(data, 1600, 0, 85)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if w != 800 || h != 600 {
		t.Errorf("got %dx%d, want unchanged 800x600", w, h)
	}
}

func TestProcessCapsHeightForPortraitCovers(t *testing.T) {
	// Portrait photo, width already under maxWidth but very tall — this is
	// exactly the case that used to slip through uncapped (post/series
	// covers are always displayed cropped, so there's no point storing
	// 2400px of height nobody sees).
	defer Reserve()()
	data := makeJPEG(t, 1200, 3600)
	out, w, h, err := Process(data, 1600, 1000, 85)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if h != 1000 {
		t.Errorf("got h=%d, want capped at 1000", h)
	}
	if w != 333 && w != 334 {
		t.Errorf("got w=%d, want ~333 (aspect ratio preserved)", w)
	}
	if _, _, err := image.Decode(bytes.NewReader(out)); err != nil {
		t.Errorf("output isn't valid image: %v", err)
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

func TestResizeSwappedCapsPostRotationWidth(t *testing.T) {
	// Pre-rotation source is portrait 2000x3000; a 90/270 rotation will follow,
	// so the *height* (3000) is what must be capped at maxWidth.
	src := image.NewNRGBA(image.Rect(0, 0, 2000, 3000))
	out := resize(src, 1600, 0, true)
	b := out.Bounds()
	if b.Dy() != 1600 {
		t.Errorf("got h=%d, want 1600 (becomes width after rotation)", b.Dy())
	}
}

// pngIHDROnly builds a minimal-but-valid PNG containing just a signature and
// an IHDR chunk, enough for image.DecodeConfig to read the declared
// dimensions without needing (or allocating) real pixel data.
func pngIHDROnly(t *testing.T, w, h uint32) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	buf.Write([]byte{137, 80, 78, 71, 13, 10, 26, 10})
	data := make([]byte, 13)
	binary.BigEndian.PutUint32(data[0:4], w)
	binary.BigEndian.PutUint32(data[4:8], h)
	data[8] = 8 // bit depth
	data[9] = 2 // color type: truecolor
	// data[10..12] compression/filter/interlace default to 0
	chunkType := []byte("IHDR")
	binary.Write(buf, binary.BigEndian, uint32(len(data)))
	buf.Write(chunkType)
	buf.Write(data)
	crc := crc32.ChecksumIEEE(append(chunkType, data...))
	binary.Write(buf, binary.BigEndian, crc)
	return buf.Bytes()
}

func TestProcessRejectsHugeImages(t *testing.T) {
	defer Reserve()()
	data := pngIHDROnly(t, 20000, 20000) // 400MP, well past the 40MP guard
	_, _, _, err := Process(data, 1600, 0, 85)
	if err == nil {
		t.Fatal("expected error for oversized image, got nil")
	}
}

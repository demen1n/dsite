package imgproc

import (
	"bytes"
	"encoding/binary"
	"errors"
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

func TestDimensionsJPEG(t *testing.T) {
	data := makeJPEG(t, 640, 480)
	w, h, err := Dimensions(data)
	if err != nil {
		t.Fatalf("Dimensions: %v", err)
	}
	if w != 640 || h != 480 {
		t.Errorf("got %dx%d, want 640x480", w, h)
	}
}

func TestProcessRejectsHugeImages(t *testing.T) {
	defer Reserve()()
	data := pngIHDROnly(t, 20000, 20000) // 400MP, way past the decode budget
	_, _, _, err := Process(data, 1600, 0, 85)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("got %v, want ErrTooLarge", err)
	}
}

// jpegHeaderOnly builds just SOI + a frame header (SOF0 or, if progressive,
// SOF2) with 3 components at the given H/V sampling for the luma one —
// enough for DecodeConfig and decodeCost; the SOS header (which DecodeConfig
// wants to see for progressive frames) is followed by no actual scan data.
func jpegHeaderOnly(w, h int, progressive bool, lumaHV byte) []byte {
	sof := byte(0xC0)
	if progressive {
		sof = 0xC2
	}
	b := []byte{0xFF, 0xD8, 0xFF, sof, 0, 17, 8, byte(h >> 8), byte(h), byte(w >> 8), byte(w), 3,
		1, lumaHV, 0,
		2, 0x11, 1,
		3, 0x11, 1,
		0xFF, 0xDA, 0, 12, 3, 1, 0, 2, 0, 3, 0, 0, 63, 0,
		0xFF, 0xD9}
	return b
}

func TestJPEGSamples(t *testing.T) {
	// image/jpeg encodes 4:2:0, so 640x480 is a full Y plane + two
	// quarter-size chroma planes.
	samples, progressive, ok := jpegSamples(makeJPEG(t, 640, 480))
	if !ok || progressive || samples != 640*480*3/2 {
		t.Errorf("got samples=%d progressive=%v ok=%v, want %d false true", samples, progressive, ok, 640*480*3/2)
	}

	samples, progressive, ok = jpegSamples(jpegHeaderOnly(100, 50, true, 0x11))
	if !ok || !progressive || samples != 104*56*3 { // padded to whole 8x8 MCUs
		t.Errorf("got samples=%d progressive=%v ok=%v, want %d true true", samples, progressive, ok, 104*56*3)
	}
}

func TestProcessRejectsHeavyProgressiveJPEG(t *testing.T) {
	defer Reserve()()
	// A 36MP progressive 4:4:4 export — fine as a pixel count, but ~550MB
	// for image/jpeg to decode.
	_, _, _, err := Process(jpegHeaderOnly(8064, 4536, true, 0x11), 1600, 0, 85)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("progressive: got %v, want ErrTooLarge", err)
	}
	// The same frame as baseline 4:2:0 fits the budget: it gets past the
	// guard and only fails for lack of scan data.
	_, _, _, err = Process(jpegHeaderOnly(8064, 4536, false, 0x22), 1600, 0, 85)
	if err == nil || errors.Is(err, ErrTooLarge) {
		t.Fatalf("baseline: got %v, want a decode error", err)
	}
}

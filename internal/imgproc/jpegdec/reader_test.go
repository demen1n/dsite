package jpegdec

import (
	"bytes"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// testFiles are image/jpeg's own test images, covering every subsampling
// ratio (including the non-standard "flex" ones), progressive, CMYK, RGB,
// restart markers and grayscale.
func testFiles(t *testing.T) map[string][]byte {
	t.Helper()
	paths, err := filepath.Glob("testdata/*.jpeg")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no test images: %v", err)
	}
	files := map[string][]byte{}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		files[filepath.Base(p)] = data
	}
	return files
}

func TestDecodeMatchesStdlib(t *testing.T) {
	for name, data := range testFiles(t) {
		want, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%s: image/jpeg: %v", name, err)
		}
		got, err := Decode(bytes.NewReader(data))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: differs from image/jpeg", name)
		}
	}
}

func TestDecodeHalfMatchesBoxFilteredStdlib(t *testing.T) {
	for name, data := range testFiles(t) {
		full, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%s: image/jpeg: %v", name, err)
		}
		half, err := DecodeHalf(bytes.NewReader(data))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		fb, hb := full.Bounds(), half.Bounds()
		if hb.Dx() != fb.Dx()/2 || hb.Dy() != fb.Dy()/2 {
			t.Errorf("%s: half is %v, want %dx%d", name, hb, fb.Dx()/2, fb.Dy()/2)
			continue
		}
		if reflect.TypeOf(half) != reflect.TypeOf(full) {
			t.Errorf("%s: half is %T, full is %T", name, half, full)
			continue
		}

		// Every plane must be the 2x2 average of image/jpeg's: exactly for
		// planes we fill directly; the CMYK/RGB conversions done afterwards
		// get ±2 for rounding in a different color space.
		maxDiff := 0
		check := func(got byte, src []byte, stride, x, y, step int) {
			i := 2*y*stride + 2*x*step
			avg := (int(src[i]) + int(src[i+step]) + int(src[i+stride]) + int(src[i+stride+step]) + 2) >> 2
			maxDiff = max(maxDiff, abs(int(got)-avg))
		}
		// checkUpsampled is check for a channel image/jpeg has already
		// upsampled sx×sy (nearest neighbour) into an interleaved image: the
		// half-res value covers a (2sx)×(2sy) block of it. Blocks running
		// past the right/bottom edge are skipped: their half-res sample also
		// averages in MCU padding the full decode never shows.
		checkUpsampled := func(got byte, src []byte, stride, x, y, sx, sy int) {
			x0, y0 := 2*sx*(x/sx), 2*sy*(y/sy)
			if x0+2*sx > fb.Dx() || y0+2*sy > fb.Dy() {
				return
			}
			sum := 0
			for yy := y0; yy < y0+2*sy; yy++ {
				for xx := x0; xx < x0+2*sx; xx++ {
					sum += int(src[yy*stride+xx*4])
				}
			}
			n := 4 * sx * sy
			maxDiff = max(maxDiff, abs(int(got)-(sum+n/2)/n))
		}
		tolerance := 0
		switch h := half.(type) {
		case *image.Gray:
			f := full.(*image.Gray)
			for y := 0; y < hb.Dy(); y++ {
				for x := 0; x < hb.Dx(); x++ {
					check(h.Pix[y*h.Stride+x], f.Pix, f.Stride, x, y, 1)
				}
			}
		case *image.YCbCr:
			f := full.(*image.YCbCr)
			for y := 0; y < hb.Dy(); y++ {
				for x := 0; x < hb.Dx(); x++ {
					check(h.Y[h.YOffset(x, y)], f.Y, f.YStride, x, y, 1)
					ci := h.COffset(x, y)
					cx, cy := ci%h.CStride, ci/h.CStride
					check(h.Cb[ci], f.Cb, f.CStride, cx, cy, 1)
					check(h.Cr[ci], f.Cr, f.CStride, cx, cy, 1)
				}
			}
		case *image.CMYK, *image.RGBA:
			tolerance = 2
			hp, hs := pix(half)
			fp, fs := pix(full)
			sub := subsampling(t, data)
			for y := 0; y < hb.Dy(); y++ {
				for x := 0; x < hb.Dx(); x++ {
					for k := 0; k < 4; k++ {
						checkUpsampled(hp[y*hs+x*4+k], fp[k:], fs, x, y, sub[k][0], sub[k][1])
					}
				}
			}
		default:
			t.Fatalf("%s: unexpected %T", name, half)
		}
		if maxDiff > tolerance {
			t.Errorf("%s: off by up to %d from the box-filtered full decode, want <= %d", name, maxDiff, tolerance)
		}
	}
}

// subsampling returns, per component of data's frame (padded to 4), how
// many times image/jpeg upsamples it horizontally and vertically.
func subsampling(t *testing.T, data []byte) (sub [4][2]int) {
	t.Helper()
	for i := 2; i+4 < len(data); {
		marker, n := data[i+1], int(data[i+2])<<8|int(data[i+3])
		if marker == 0xC0 || marker == 0xC1 || marker == 0xC2 {
			nc := int(data[i+9])
			var hmax, vmax int
			for c := 0; c < nc; c++ {
				hv := data[i+11+3*c]
				hmax, vmax = max(hmax, int(hv>>4)), max(vmax, int(hv&15))
			}
			for c := range sub {
				sub[c] = [2]int{1, 1}
				if c < nc {
					hv := data[i+11+3*c]
					sub[c] = [2]int{hmax / int(hv>>4), vmax / int(hv&15)}
				}
			}
			return sub
		}
		i += 2 + n
	}
	t.Fatal("no SOF")
	return sub
}

func pix(m image.Image) ([]byte, int) {
	switch m := m.(type) {
	case *image.CMYK:
		return m.Pix, m.Stride
	case *image.RGBA:
		return m.Pix, m.Stride
	}
	panic("unreachable")
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func TestDecodeConfig(t *testing.T) {
	data := testFiles(t)["video-001.jpeg"]
	cfg, err := DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := jpeg.DecodeConfig(bytes.NewReader(data))
	if cfg != want {
		t.Errorf("got %+v, want %+v", cfg, want)
	}
}

func TestDecodeErrors(t *testing.T) {
	data := testFiles(t)["video-001.progressive.jpeg"]
	for name, in := range map[string][]byte{
		"empty":     nil,
		"not jpeg":  []byte("GIF89a"),
		"truncated": data[:len(data)/3],
	} {
		if _, err := DecodeHalf(bytes.NewReader(in)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

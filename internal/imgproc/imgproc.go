// Package imgproc decodes uploaded images, applies EXIF orientation,
// downscales them with a high-quality filter, and re-encodes to JPEG —
// replacing the old browser-side canvas resize with a consistent,
// server-controlled pipeline.
package imgproc

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"runtime"

	"golang.org/x/image/draw"

	_ "image/gif"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

// maxSourcePixels guards against pathological uploads (decompression-bomb
// style images) blowing the process' memory budget before we ever get to
// resize them down to something reasonable. 80MP covers real camera/phone
// output with margin (even 48MP "high-res mode" phone shots) while keeping
// peak memory for a single upload around ~350MB, safely inside the
// container's 512MB limit alongside the rest of the app.
const maxSourcePixels = 80_000_000 // ~80MP

// procSem limits image processing to one image at a time process-wide. The
// container this runs in is memory- and CPU-constrained (512MB, 0.5 CPU);
// two or three uploads landing back-to-back (e.g. dropping several photos
// into a post) can otherwise stack their peak memory and blow the limit even
// though each one individually fits comfortably. Concurrent HTTP requests
// still queue normally — they just wait their turn here instead of all
// decoding/resizing at once.
var procSem = make(chan struct{}, 1)

// Reserve blocks until the single processing slot is free and returns a
// function that releases it. Process does not acquire this itself — callers
// must call Reserve before reading their source file into memory and hold it
// through the call to Process (or the raw-save path for formats Process
// doesn't touch, like GIF). Acquiring only around Process would still let
// several concurrent requests each read their full multi-MB source into RAM
// while queued, stacking peak memory across requests instead of bounding it
// to roughly one file's worth at a time.
func Reserve() (release func()) {
	procSem <- struct{}{}
	return func() {
		// GC before releasing the slot: Go's default pacer only collects once
		// the heap has doubled since the last cycle, so back-to-back calls can
		// pile up uncollected garbage from the previous one even though they
		// never run concurrently. Forcing a sweep here means the next queued
		// upload starts from a clean slate instead of stacking on top of what
		// came before.
		runtime.GC()
		<-procSem
	}
}

// Process decodes data (jpeg/png/gif/webp), corrects EXIF rotation, downscales
// so the result is at most maxWidth wide and (if maxHeight > 0) at most
// maxHeight tall — never upscales — and re-encodes as JPEG at the given
// quality (1-100). It returns the encoded bytes and final dimensions.
//
// maxHeight is normally 0 (unbounded): a width-only cap keeps a photo's
// natural aspect ratio, which is what you want for anything displayed in
// full (gallery photos, inline post images). Pass maxHeight > 0 for images
// that are always shown cropped into a bounded box (post/series covers,
// avatar) — otherwise a portrait-oriented photo only gets capped on its
// (already short) width and keeps a needlessly tall, heavy long edge that
// nothing ever displays.
//
// Resize runs before the EXIF-orientation rotate: rotating first would mean
// allocating a full-resolution copy of the source (up to 4 bytes/pixel) just
// to immediately shrink it. Resizing first keeps that copy small regardless
// of how large the uploaded photo is.
//
// Callers must hold a Reserve() slot for the duration of this call.
func Process(data []byte, maxWidth, maxHeight, quality int) (out []byte, w, h int, err error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("decode config: %w", err)
	}
	if cfg.Width*cfg.Height > maxSourcePixels {
		return nil, 0, 0, fmt.Errorf("image too large: %dx%d", cfg.Width, cfg.Height)
	}

	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("decode: %w", err)
	}

	o := jpegOrientation(data)
	img := resize(src, maxWidth, maxHeight, o >= 5) // o 5-8 swap width/height on display
	img = applyOrientation(img, o)

	buf := new(bytes.Buffer)
	if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, 0, 0, fmt.Errorf("encode: %w", err)
	}
	b := img.Bounds()
	return buf.Bytes(), b.Dx(), b.Dy(), nil
}

// Dimensions decodes just enough of data to report its pixel dimensions,
// without fully decoding or transforming the image.
func Dimensions(data []byte) (w, h int, err error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

// resize scales src so its displayed dimensions (the source's width/height,
// swapped if a 90/270 EXIF rotation is coming next) fit within maxWidth and
// (if set) maxHeight. It never upscales. swapped lets the caller apply
// orientation after resizing while still capping what the user will
// actually see.
func resize(src image.Image, maxWidth, maxHeight int, swapped bool) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	displayW, displayH := w, h
	if swapped {
		displayW, displayH = h, w
	}

	scale := 1.0
	if displayW > maxWidth {
		scale = float64(maxWidth) / float64(displayW)
	}
	if maxHeight > 0 && displayH > maxHeight {
		if hScale := float64(maxHeight) / float64(displayH); hScale < scale {
			scale = hScale
		}
	}
	if scale >= 1 {
		return src
	}
	newW := int(math.Round(float64(w) * scale))
	newH := int(math.Round(float64(h) * scale))

	// draw.CatmullRom is a separable two-pass filter: its first pass keeps
	// the full source height while only shrinking the width, so for a large
	// ratio (e.g. a 48MP phone photo down to 1600px) that intermediate
	// buffer alone can run into hundreds of MB. Pre-shrink with a cheap box
	// filter (which needs no such intermediate) so CatmullRom only ever has
	// to bridge at most ~2x, keeping peak memory proportional to the output.
	src = boxShrinkToward(src, newW, newH)
	b = src.Bounds()

	dst := image.NewNRGBA(image.Rect(0, 0, newW, newH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return dst
}

// boxShrinkToward halves src's dimensions by averaging 2x2 blocks, repeating
// until doing so again would drop below targetW/targetH, so the final
// hand-off to CatmullRom is never more than ~2x oversized.
func boxShrinkToward(src image.Image, targetW, targetH int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	for w/2 >= targetW && h/2 >= targetH {
		src = boxHalve(src)
		b = src.Bounds()
		w, h = b.Dx(), b.Dy()
	}
	return src
}

// boxHalve averages each 2x2 block of src into one pixel of the result.
//
// The generic path goes through the image.Image/color.Color interfaces,
// which box a small struct on the heap for *every pixel read and write* —
// for a large photo that's tens of millions of tiny allocations, enough
// garbage to spike RSS well past what the data itself needs before the GC
// catches up. JPEG source (*image.YCbCr) and our own intermediate
// (*image.NRGBA) — the two types this ever actually sees in practice — get
// dedicated byte-level fast paths instead.
func boxHalve(src image.Image) image.Image {
	switch s := src.(type) {
	case *image.YCbCr:
		return boxHalveYCbCr(s)
	case *image.NRGBA:
		return boxHalveNRGBA(s)
	default:
		return boxHalveGeneric(src)
	}
}

func boxHalveYCbCr(src *image.YCbCr) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx()/2, b.Dy()/2
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		sy0, sy1 := b.Min.Y+y*2, b.Min.Y+y*2+1
		drow := dst.Pix[y*dst.Stride:]
		for x := 0; x < w; x++ {
			sx0, sx1 := b.Min.X+x*2, b.Min.X+x*2+1
			var rs, gs, bs int
			for _, py := range [2]int{sy0, sy1} {
				for _, px := range [2]int{sx0, sx1} {
					yy := src.Y[src.YOffset(px, py)]
					ci := src.COffset(px, py)
					r, g, bl := color.YCbCrToRGB(yy, src.Cb[ci], src.Cr[ci])
					rs += int(r)
					gs += int(g)
					bs += int(bl)
				}
			}
			o := x * 4
			drow[o] = uint8(rs / 4)
			drow[o+1] = uint8(gs / 4)
			drow[o+2] = uint8(bs / 4)
			drow[o+3] = 255
		}
	}
	return dst
}

func boxHalveNRGBA(src *image.NRGBA) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx()/2, b.Dy()/2
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		row0 := src.Pix[(b.Min.Y+y*2)*src.Stride:]
		row1 := src.Pix[(b.Min.Y+y*2+1)*src.Stride:]
		drow := dst.Pix[y*dst.Stride:]
		for x := 0; x < w; x++ {
			so := (b.Min.X + x*2) * 4
			do := x * 4
			for c := 0; c < 4; c++ {
				sum := int(row0[so+c]) + int(row0[so+4+c]) + int(row1[so+c]) + int(row1[so+4+c])
				drow[do+c] = uint8(sum / 4)
			}
		}
	}
	return dst
}

// boxHalveGeneric handles any other image.Image (PNG's *image.RGBA, GIF's
// *image.Paletted, etc.) — rare for the very large sources this matters for,
// so the interface-boxing overhead is acceptable here.
func boxHalveGeneric(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx()/2, b.Dy()/2
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		sy := b.Min.Y + y*2
		for x := 0; x < w; x++ {
			sx := b.Min.X + x*2
			r0, g0, b0, a0 := src.At(sx, sy).RGBA()
			r1, g1, b1, a1 := src.At(sx+1, sy).RGBA()
			r2, g2, b2, a2 := src.At(sx, sy+1).RGBA()
			r3, g3, b3, a3 := src.At(sx+1, sy+1).RGBA()
			dst.Set(x, y, color.RGBA64{
				R: uint16((r0 + r1 + r2 + r3) / 4),
				G: uint16((g0 + g1 + g2 + g3) / 4),
				B: uint16((b0 + b1 + b2 + b3) / 4),
				A: uint16((a0 + a1 + a2 + a3) / 4),
			})
		}
	}
	return dst
}

// Package imgproc decodes uploaded images, applies EXIF orientation,
// downscales them with a high-quality filter, and re-encodes to JPEG —
// replacing the old browser-side canvas resize with a consistent,
// server-controlled pipeline.
package imgproc

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"math"

	"golang.org/x/image/draw"

	_ "image/gif"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

// Process decodes data (jpeg/png/gif/webp), corrects EXIF rotation, downscales
// so the width is at most maxWidth (never upscales), and re-encodes as JPEG
// at the given quality (1-100). It returns the encoded bytes and final
// dimensions.
func Process(data []byte, maxWidth, quality int) (out []byte, w, h int, err error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("decode: %w", err)
	}

	img := applyOrientation(src, jpegOrientation(data))
	img = resize(img, maxWidth)

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

func resize(src image.Image, maxWidth int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxWidth {
		return src
	}
	newW := maxWidth
	newH := int(math.Round(float64(h) * float64(maxWidth) / float64(w)))
	dst := image.NewNRGBA(image.Rect(0, 0, newW, newH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return dst
}

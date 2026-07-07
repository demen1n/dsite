package imgproc

import (
	"encoding/binary"
	"image"
	"image/color"
)

// jpegOrientation scans a JPEG's APP1/Exif segment for the Orientation tag
// (0x0112) and returns its value (1-8), or 1 (no-op) if absent or the input
// isn't a JPEG with EXIF data.
func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			break
		}
		marker := data[i+1]
		if marker == 0xD8 || marker == 0xD9 || (marker >= 0xD0 && marker <= 0xD7) {
			i += 2
			continue
		}
		if marker == 0xDA { // start of scan: no more metadata segments follow
			break
		}
		segLen := int(data[i+2])<<8 | int(data[i+3])
		if segLen < 2 || i+2+segLen > len(data) {
			break
		}
		if marker == 0xE1 {
			if o := parseExifOrientation(data[i+4 : i+2+segLen]); o != 0 {
				return o
			}
		}
		i += 2 + segLen
	}
	return 1
}

func parseExifOrientation(seg []byte) int {
	if len(seg) < 10 || string(seg[0:6]) != "Exif\x00\x00" {
		return 0
	}
	tiff := seg[6:]
	if len(tiff) < 8 {
		return 0
	}
	var bo binary.ByteOrder
	switch string(tiff[0:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0
	}
	ifdOffset := bo.Uint32(tiff[4:8])
	if int(ifdOffset)+2 > len(tiff) {
		return 0
	}
	p := tiff[ifdOffset:]
	count := int(bo.Uint16(p[0:2]))
	p = p[2:]
	for j := 0; j < count && (j+1)*12 <= len(p); j++ {
		entry := p[j*12 : j*12+12]
		if bo.Uint16(entry[0:2]) == 0x0112 {
			if val := bo.Uint16(entry[8:10]); val >= 1 && val <= 8 {
				return int(val)
			}
		}
	}
	return 0
}

// applyOrientation rotates/flips img per the EXIF orientation values 1-8.
// See the EXIF spec's Orientation tag for the canonical transform table.
func applyOrientation(src image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	at := func(x, y int) color.Color { return src.At(b.Min.X+x, b.Min.Y+y) }

	switch o {
	case 2: // mirror horizontal
		dst := image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(w-1-x, y, at(x, y))
			}
		}
		return dst
	case 3: // rotate 180
		dst := image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(w-1-x, h-1-y, at(x, y))
			}
		}
		return dst
	case 4: // mirror vertical
		dst := image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(x, h-1-y, at(x, y))
			}
		}
		return dst
	case 5: // transpose
		dst := image.NewNRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(y, x, at(x, y))
			}
		}
		return dst
	case 6: // rotate 90 CW
		dst := image.NewNRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(h-1-y, x, at(x, y))
			}
		}
		return dst
	case 7: // transverse
		dst := image.NewNRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(h-1-y, w-1-x, at(x, y))
			}
		}
		return dst
	default: // 8: rotate 270 CW (90 CCW)
		dst := image.NewNRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(y, w-1-x, at(x, y))
			}
		}
		return dst
	}
}

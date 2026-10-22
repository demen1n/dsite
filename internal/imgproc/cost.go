package imgproc

import (
	"encoding/binary"
	"image"
	"image/color"
)

// decodeCost estimates the peak bytes Process allocates for data: the decoded
// image plus the decoder's scratch state, plus the first box-filter halving
// in resize (an NRGBA a quarter of the source's area, i.e. 1 byte per source
// pixel).
func decodeCost(data []byte, cfg image.Config, format string) int64 {
	px := int64(cfg.Width) * int64(cfg.Height)
	if format == "jpeg" {
		if samples, progressive, ok := jpegSamples(data); ok {
			perSample := int64(1) // decoded YCbCr/Gray plane
			if progressive {
				perSample += 4 // int32 DCT coefficients kept across scans
			}
			return samples*perSample + px
		}
	}
	bpp := int64(4)
	switch cfg.ColorModel {
	case color.RGBA64Model, color.NRGBA64Model:
		bpp = 8
	}
	return px*bpp + px
}

// jpegSamples walks data's markers up to the frame header (SOF) and returns
// how many samples image/jpeg will allocate for it — every component padded
// out to whole MCUs, as the decoder does — and whether it's progressive.
func jpegSamples(data []byte) (samples int64, progressive, ok bool) {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 0, false, false
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return 0, false, false
		}
		marker := data[i+1]
		if marker == 0xFF { // fill byte
			i++
			continue
		}
		if marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) { // no length
			i += 2
			continue
		}
		if marker == 0xDA || marker == 0xD9 { // SOS/EOI before any SOF
			return 0, false, false
		}
		n := int(binary.BigEndian.Uint16(data[i+2:]))
		seg := data[i+4:]
		if n < 2 || len(seg) < n-2 {
			return 0, false, false
		}
		seg = seg[:n-2]
		if marker == 0xC0 || marker == 0xC1 || marker == 0xC2 {
			return sofSamples(seg, marker == 0xC2)
		}
		i += 2 + n
	}
	return 0, false, false
}

func sofSamples(seg []byte, progressive bool) (int64, bool, bool) {
	if len(seg) < 6 {
		return 0, false, false
	}
	h := int64(binary.BigEndian.Uint16(seg[1:]))
	w := int64(binary.BigEndian.Uint16(seg[3:]))
	nc := int(seg[5])
	if nc == 0 || len(seg) < 6+3*nc {
		return 0, false, false
	}
	var hmax, vmax int64 = 1, 1
	for c := 0; c < nc; c++ {
		hv := seg[6+3*c+1]
		hmax = max(hmax, int64(hv>>4))
		vmax = max(vmax, int64(hv&0x0F))
	}
	mcuW := (w + 8*hmax - 1) / (8 * hmax)
	mcuH := (h + 8*vmax - 1) / (8 * vmax)
	var samples int64
	for c := 0; c < nc; c++ {
		hv := seg[6+3*c+1]
		samples += mcuW * int64(hv>>4) * 8 * mcuH * int64(hv&0x0F) * 8
	}
	return samples, progressive, true
}

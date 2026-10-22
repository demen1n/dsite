package imgproc

import (
	"encoding/binary"
	"image"
	"image/color"
)

// memCost estimates the peak live bytes Process needs for data (beyond data
// itself): the larger of the decode phase — the decoded image plus the
// decoder's scratch state — and the resize phase that follows, when the
// decoder's scratch is garbage but the decoded image, the box-filter
// intermediates and CatmullRom's buffers are live. half means a JPEG decoded
// by jpegdec.DecodeHalf; newW×newH is resize's target, or 0×0 if the image
// is kept at its size.
func memCost(data []byte, cfg image.Config, format string, half bool, newW, newH int) int64 {
	w, h := int64(cfg.Width), int64(cfg.Height)
	var img, scratch int64
	if samples, progressive, ok := jpegSamples(data); format == "jpeg" && ok {
		img = samples // decoded YCbCr/Gray planes, 1 byte per sample
		if progressive {
			scratch = 2 * samples // int16 DCT coefficients kept across scans
		}
	} else {
		bpp := int64(4)
		switch cfg.ColorModel {
		case color.RGBA64Model, color.NRGBA64Model:
			bpp = 8
		}
		img = w * h * bpp
	}
	if half {
		img, w, h = img/4, w/2, h/2
	}
	cost := img + scratch
	if newW > 0 {
		cost = max(cost, img+resizeCost(w, h, int64(newW), int64(newH)))
	}
	return cost
}

// resizeCost mirrors resize's allocations shrinking a w×h image to newW×newH:
// boxShrinkToward's NRGBA halvings (each live alongside the previous one
// while it's being made), then CatmullRom's float64 scratch — 32 bytes per
// pixel of a newW-wide, source-tall intermediate, the biggest of the lot for
// a portrait photo — plus the NRGBA result.
func resizeCost(w, h, newW, newH int64) int64 {
	var prev, cur, peak int64
	for w/2 >= newW && h/2 >= newH {
		w, h = w/2, h/2
		prev, cur = cur, w*h*4
		peak = max(peak, prev+cur)
	}
	return max(peak, cur+newW*h*32+newW*newH*4)
}

// jpegSamples walks data's markers up to the frame header (SOF) and returns
// how many samples the decoder allocates for it — every component padded
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

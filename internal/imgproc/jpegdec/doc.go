// Package jpegdec is a fork of the standard library's image/jpeg decoder
// (Go 1.27.3; see LICENSE) tuned for decoding big photos in little memory,
// for internal/imgproc. Two changes, both marked where they're made:
//
//   - A progressive image's DCT coefficients are kept between scans as int16
//     (type coeffs) rather than int32 blocks, as libjpeg does. That buffer is
//     the bulk of a progressive decode — 4 bytes per sample upstream, which
//     for a 36MP 4:4:4 photo alone is ~440MB — and this halves it.
//   - DecodeHalf writes every block straight into a half-resolution image
//     (2x2 box average), for callers that are going to shrink the photo by
//     at least 2x anyway: the full-resolution image is never allocated.
//
// The encoder and everything else unrelated to decoding are left out.
// Decode at full resolution is otherwise identical to image/jpeg's.
package jpegdec

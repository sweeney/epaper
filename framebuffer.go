package epaper

import (
	"fmt"
	"image"
)

// bitsPerPixel is the controller's wire format: two bits per pixel, so four
// pixels to a byte and at most four distinct inks.
//
// Derived from Pimoroni inky 2.5.0, inky_jd79668.py Inky.show(), reproduced at
// reference/vendor-inky/.
const (
	bitsPerPixel   = 2
	pixelsPerByte  = 8 / bitsPerPixel
	maxPaletteSize = 1 << bitsPerPixel
)

// Pack converts an image into the panel's framebuffer: two bits per pixel,
// four pixels to a byte, most significant pixel first.
//
//	byte = (p0&3)<<6 | (p1&3)<<4 | (p2&3)<<2 | (p3&3)
//
// The pixels are taken in row-major order as one continuous stream, so on a
// panel whose width is not a multiple of four a byte spans the row boundary.
// That is what the controller expects, and what the vendor library does. A
// final partial byte is zero-padded; those bits address pixels the panel does
// not have.
//
// A 400x300 panel packs to exactly 30,000 bytes.
//
// Pack refuses an image it cannot represent faithfully rather than masking the
// index and drawing the wrong colour: more than four palette entries returns
// [ErrPaletteTooLarge], and a pixel whose index is not defined by its own
// palette returns [ErrBadImage].
//
// Derived from Pimoroni inky 2.5.0, inky_jd79668.py Inky.show(), reproduced at
// reference/vendor-inky/.
func Pack(img *image.Paletted) ([]byte, error) {
	if img == nil {
		return nil, fmt.Errorf("epaper: pack: image is nil: %w", ErrBadImage)
	}
	b := img.Bounds()
	if b.Empty() {
		return nil, fmt.Errorf("epaper: pack: image is empty: %w", ErrBadImage)
	}
	if len(img.Palette) > maxPaletteSize {
		return nil, fmt.Errorf("epaper: pack: image has %d colours, this format addresses at most %d: %w",
			len(img.Palette), maxPaletteSize, ErrPaletteTooLarge)
	}

	w, h := b.Dx(), b.Dy()
	total := w * h
	out := make([]byte, (total+pixelsPerByte-1)/pixelsPerByte)

	limit := uint8(len(img.Palette))

	// The byte under construction is accumulated in a register and stored
	// once, rather than OR-ing each pixel straight into out. Four
	// read-modify-writes per byte measured about 1.8x slower than one store,
	// on 120,000 pixels.
	var cur byte
	n := 0 // index into the flattened pixel stream

	for y := b.Min.Y; y < b.Max.Y; y++ {
		// Index by row rather than walking Pix, because a SubImage has a
		// stride wider than its width and the padding is not pixels.
		row := img.Pix[img.PixOffset(b.Min.X, y):][:w]
		for x, v := range row {
			if v >= limit {
				return nil, fmt.Errorf("epaper: pack: pixel (%d,%d) has index %d but the palette defines %d colours: %w",
					b.Min.X+x, y, v, limit, ErrBadImage)
			}
			// 0, 1, 2, 3 pixels into the byte shift by 6, 4, 2, 0.
			cur |= v << ((pixelsPerByte - 1 - n%pixelsPerByte) * bitsPerPixel)
			n++
			if n%pixelsPerByte == 0 {
				out[n/pixelsPerByte-1] = cur
				cur = 0
			}
		}
	}

	// A trailing partial byte, zero-padded by construction.
	if n%pixelsPerByte != 0 {
		out[n/pixelsPerByte] = cur
	}

	return out, nil
}

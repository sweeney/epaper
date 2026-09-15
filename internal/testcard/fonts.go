package testcard

import (
	"fmt"
	"sync"

	"github.com/sweeney/epaper/render"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/inconsolata"
	"golang.org/x/image/font/opentype"
)

// bitmapCeiling is the size above which a scaled outline is good enough.
//
// Below it, outlines lose: a stem is about a pixel wide, lands at an arbitrary
// sub-pixel position, and after thresholding some stems are one pixel and
// their neighbours two. Compared on the real panel — see render.FontFamily.
const bitmapCeiling = 17

// newFamily returns a FontFamily that uses hand-drawn bitmap faces for small
// text and scales the supplied outline font above them.
//
// This mixes a monospace bitmap face with a proportional outline, which on a
// test card is the right trade: legibility at 8px matters more than a
// consistent typeface.
func newFamily(ttf []byte) (render.FontFamily, error) {
	parsed, err := opentype.Parse(ttf)
	if err != nil {
		return nil, fmt.Errorf("testcard: parsing font: %w", err)
	}
	var mu sync.Mutex
	cache := map[int]font.Face{}

	return func(size int) (font.Face, error) {
		switch {
		case size < 14:
			return basicfont.Face7x13, nil
		case size < bitmapCeiling:
			return inconsolata.Regular8x16, nil
		}

		mu.Lock()
		defer mu.Unlock()
		if f, ok := cache[size]; ok {
			return f, nil
		}
		// DPI 72 makes one point equal one pixel, so size means pixels.
		// Hinting is off: above the bitmap ceiling it changes little, and
		// x/image's hinting distorts glyphs more than it helps.
		f, err := opentype.NewFace(parsed, &opentype.FaceOptions{
			Size: float64(size), DPI: 72, Hinting: font.HintingNone,
		})
		if err != nil {
			return nil, fmt.Errorf("testcard: face at %dpx: %w", size, err)
		}
		cache[size] = f
		return f, nil
	}, nil
}

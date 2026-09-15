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
// Below it, outlines lose. A stem is about a pixel wide, lands at an arbitrary
// sub-pixel position, and after thresholding some stems come out one pixel and
// their neighbours two — which reads as uneven spacing. Settled by drawing the
// candidates on a real panel; see [render.FontFamily].
const bitmapCeiling = 17

// Fonts returns the card's default faces, which need no font file.
//
// Small sizes are hand-drawn bitmap faces from golang.org/x/image —
// [basicfont.Face7x13] below 14px, [inconsolata.Regular8x16] above — because
// those were designed on a pixel grid and stay crisp on a panel with no
// intermediate tones to soften an edge with.
//
// Large sizes are the same bitmap face integer-scaled with [render.ScaleFace],
// so a heading is exactly as crisp as the body text rather than being the one
// blurry thing on the panel. The trade is that sizes come in steps: asking for
// 44px gets you 32px, the largest whole multiple that fits.
//
// It embeds nothing of its own — those faces are already linked in, because
// this library depends on golang.org/x/image for [font.Face] regardless.
func Fonts() render.FontFamily {
	var mu sync.Mutex
	scaled := map[int]font.Face{}

	return func(size int) (font.Face, error) {
		switch {
		case size < 14:
			return basicfont.Face7x13, nil
		case size < 2*inconsolataHeight:
			return inconsolata.Regular8x16, nil
		}

		// Whole multiples only: a fractional scale would put glyph edges
		// between pixels, which is the problem this avoids.
		n := size / inconsolataHeight

		mu.Lock()
		defer mu.Unlock()
		if f, ok := scaled[n]; ok {
			return f, nil
		}
		f := render.ScaleFace(inconsolata.Regular8x16, n)
		scaled[n] = f
		return f, nil
	}
}

// inconsolataHeight is the pixel height of inconsolata.Regular8x16, and so the
// step between the scaled sizes [Fonts] can offer.
const inconsolataHeight = 16

// FontsWith is [Fonts] for the small sizes, scaling the supplied TrueType font
// above the point where outlines start to work — around 17px.
//
// Use it when the card's heading should be in your own typeface. The small
// labels stay bitmap on purpose: that is the whole finding behind this
// package's font choices, and overriding it makes them worse, not different.
func FontsWith(ttf []byte) (render.FontFamily, error) {
	parsed, err := opentype.Parse(ttf)
	if err != nil {
		return nil, fmt.Errorf("testcard: parsing font: %w", err)
	}
	bitmap := Fonts()

	var mu sync.Mutex
	cache := map[int]font.Face{}

	return func(size int) (font.Face, error) {
		if size < bitmapCeiling {
			return bitmap(size)
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

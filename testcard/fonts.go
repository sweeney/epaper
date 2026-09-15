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
// Everything it hands back is a hand-drawn bitmap face from
// golang.org/x/image: [basicfont.Face7x13] below 14px and
// [inconsolata.Regular8x16] above. Both were designed on a pixel grid, which
// is why they stay crisp on a panel that has no intermediate tones to soften
// an edge with.
//
// It embeds nothing of its own — those faces are already linked in, because
// this library depends on golang.org/x/image for [font.Face] regardless.
func Fonts() render.FontFamily {
	return func(size int) (font.Face, error) {
		if size < 14 {
			return basicfont.Face7x13, nil
		}
		return inconsolata.Regular8x16, nil
	}
}

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

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
// [basicfont.Face7x13] below 17px, [inconsolata.Regular8x16] above — because
// those were designed on a pixel grid and stay crisp on a panel with no
// intermediate tones to soften an edge with.
//
// Large sizes are the same bitmap face integer-scaled with [render.ScaleFace],
// so a heading is exactly as crisp as the body text rather than being the one
// blurry thing on the panel. The trade is that sizes come in steps of 17px:
// asking for 48px gets you 34px, the largest whole multiple that fits. The
// face returned is never TALLER than the size requested, which is what
// [render.Canvas.TextFitted] relies on — with one unavoidable exception:
// nothing here is smaller than 13px, so a request below that gets the 13px
// face. TextFitted will then correctly report that the box cannot hold text,
// which on this hardware it cannot: the bench found 10px to be the floor for
// legibility and 8px unreadable.
//
// It embeds nothing of its own — those faces are already linked in, because
// this library depends on golang.org/x/image for [font.Face] regardless.
func Fonts() render.FontFamily {
	var mu sync.Mutex
	scaled := map[int]font.Face{}

	return func(size int) (font.Face, error) {
		switch {
		case size < inconsolataHeight:
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

// fontSizes is every distinct size [Fonts] can produce, ascending.
//
// It is the ladder, and it is short: 13 from basicfont, then whole multiples
// of inconsolata's 17. Everything between rounds DOWN, so asking for 48 gets
// 34 and asking for 50 also gets 34.
//
// The list is here rather than computed so that it reads as what it is — a
// hard constraint on layout — and TestFontSizesMatchesTheFamily sweeps the
// family to prove the two cannot drift apart.
var fontSizes = []int{13, 17, 2 * inconsolataHeight, 3 * inconsolataHeight, 4 * inconsolataHeight}

// FontSizes returns the sizes [Fonts] can actually produce, ascending:
// 13, 17, 34, 51, 68.
//
// This exists because the ladder is a LAYOUT constraint, and a layout that
// discovers it by experiment discovers it late. The gap between 34 and 51 is
// 17 pixels wide and lands exactly where a headline wants to be: on a 400px
// panel "WAIT IF YOU CAN" is 240px at 34 and 360px at 51, so a headline with
// anything beside it has one usable size and not two.
//
// Design to these, or supply your own face with [FontsWith]. See [Fonts] for
// why the steps are what they are — in short, an integer-scaled bitmap stays
// crisp on a panel with no intermediate tones and a fractionally scaled one
// does not, so half steps are not available at any price.
//
// The result is a fresh slice; callers may sort or truncate it freely.
func FontSizes() []int {
	out := make([]int, len(fontSizes))
	copy(out, fontSizes)
	return out
}

// LargestFontSizeFor returns the biggest size from [FontSizes] whose line
// height fits in height pixels, or 0 if none does.
//
// This is the question a layout actually asks, and asking it before drawing
// beats calling [render.Canvas.TextFitted] and discovering the answer from a
// rendered result — particularly when the text must NOT be resized to fit,
// which on a display that refreshes every few minutes is the usual case.
//
// Zero means the box is shorter than 13px, which no bundled face can fill. The
// bench found 10px to be the legibility floor on this hardware and 8px
// unreadable, so there is nothing sensible below the bottom of this ladder.
func LargestFontSizeFor(height int) int {
	best := 0
	for _, s := range fontSizes {
		if s <= height {
			best = s
		}
	}
	return best
}

// inconsolataHeight is the LINE height of inconsolata.Regular8x16 — ascent 14
// plus descent 3 — and so the step between the scaled sizes [Fonts] offers.
//
// It is 17, not the 16 in the face's name: "8x16" describes the glyph cell,
// not the metrics. Taking the name at face value made Fonts(48) hand back a
// 51px face, overshooting what the caller asked for. Caught by a doc example.
const inconsolataHeight = 17

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

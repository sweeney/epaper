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

// bases are the hand-drawn faces the family scales, largest first.
//
// TWO of them, and that is what makes the ladder usable. Each contributes
// whole multiples of its own line height — 17, 34, 51, 68 from Inconsolata and
// 13, 26, 39, 52, 65 from basicfont — and interleaving them halves the worst
// gap. See [FontSizes] for why that matters and what it costs.
var bases = []struct {
	face font.Face
	lh   int
}{
	{inconsolata.Regular8x16, inconsolataHeight},
	{basicfont.Face7x13, basicfontHeight},
}

// Fonts returns the card's default faces, which need no font file.
//
// Every face is a hand-drawn bitmap from golang.org/x/image, integer-scaled
// with [render.ScaleFace] when it needs to be bigger. Bitmaps because they
// were designed on a pixel grid and stay crisp on a panel with no intermediate
// tones to soften an edge with; WHOLE multiples because a fractional scale
// puts glyph edges between pixels, which is the same problem in a different
// coat. A heading is therefore exactly as crisp as the body text rather than
// being the one blurry thing on the panel.
//
// The face returned is never TALLER than the size requested — see
// [render.FontFamily], where that is a contract and not merely a habit — so
// asking for the height of a box gets the largest face that fits it. The one
// exception is a request below 13px, where there is nothing smaller to give:
// the 13px face comes back and the caller has to notice.
// [render.Canvas.TextFitted] does, by measuring.
//
// Sizes still come in steps, because whole multiples of a bitmap are all there
// is. [FontSizes] lists them and explains what that means for a layout; the
// short version is that the steps are 4 to 13 pixels apart, not smooth.
//
// It embeds nothing of its own — those faces are already linked in, because
// this library depends on golang.org/x/image for [font.Face] regardless.
func Fonts() render.FontFamily {
	var mu sync.Mutex
	scaled := map[int]font.Face{}

	return func(size int) (font.Face, error) {
		// The largest whole multiple of any base that fits. Bases are ordered
		// largest-first, so an exact tie goes to Inconsolata, which is the
		// face this family has always used at 17 and above.
		bestLH, bestBase, bestN := 0, -1, 0
		for i, b := range bases {
			n := size / b.lh
			if n >= 1 && n*b.lh > bestLH {
				bestLH, bestBase, bestN = n*b.lh, i, n
			}
		}
		if bestBase < 0 {
			// Below the smallest face. Documented above: hand back the
			// smallest there is and let the caller measure.
			return basicfont.Face7x13, nil
		}
		if bestN == 1 {
			return bases[bestBase].face, nil
		}

		mu.Lock()
		defer mu.Unlock()
		if f, ok := scaled[bestLH]; ok {
			return f, nil
		}
		f := render.ScaleFace(bases[bestBase].face, bestN)
		scaled[bestLH] = f
		return f, nil
	}
}

// fontSizes is every distinct size [Fonts] can produce, ascending: whole
// multiples of both bases, interleaved.
//
// Computed rather than written out, so it cannot disagree with the family.
// TestFontSizesMatchesTheFamily sweeps Fonts and checks every line height it
// produces appears here.
var fontSizes = func() []int {
	const limit = 80 // past any plausible panel; 78 is the last rung under it
	seen := map[int]bool{}
	var out []int
	for s := 1; s <= limit; s++ {
		for _, b := range bases {
			if s%b.lh == 0 && !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
}()

// FontSizes returns the sizes [Fonts] can actually produce, ascending:
// 13, 17, 26, 34, 39, 51, 52, 65, 68, 78.
//
// This exists because the ladder is a LAYOUT constraint, and a layout that
// discovers it by experiment discovers it late.
//
// # Why there are two typefaces in it
//
// The ladder used to be 13, 17, 34, 51, 68 — Inconsolata's 17 scaled, with
// basicfont's 13 underneath it. That left a 17px hole between 34 and 51,
// exactly where a headline wants to be, and a consumer found it the hard way
// (issue #4): on a 400px panel "WAIT IF YOU CAN" beside "13:08" is 320px at 34
// and 480px at 51, so there was one usable headline size and not two.
//
// Scaling basicfont as well fills it. The rungs interleave, the worst gap
// halves from 17px to 13px, and 39 exists where 40 was wanted.
//
// The cost is that two adjacent rungs can be different typefaces — 34 is
// Inconsolata and 39 is basicfont. That is a smaller price than it first
// appears, and the reason is worth stating plainly: this family ALREADY mixed
// them. It has always returned basicfont below 17 and Inconsolata above, and
// the test card draws its own legibility ladder at 17 and 13 in the same
// circle. Adding rungs does not put a second typeface on the panel; there were
// always two. What changes is that a layout can now land on either.
//
// If that matters for a particular screen, pick one rung and stay on it — and
// note that a size and its double are always the same face, since both come
// from the same base.
//
// # The ladder is often not the real constraint
//
// Worth measuring before assuming a missing rung is the problem. These faces
// are MONOSPACE, and a monospace advance is wide relative to its height, so a
// long line runs out of width before it runs out of ladder.
//
// The case from issue #4 — a headline beside a clock on a 400px panel:
//
//	34px (Inconsolata x2)   240 + 80 = 320   fits
//	39px (basicfont x3)     315 + 105 = 420  does not
//	51px (Inconsolata x3)   360 + 120 = 480  does not
//
// So the 39 this ladder gained does not help that layout, even though it is
// the size that was asked for. basicfont at 39 is 21px per glyph against
// Inconsolata's 16 at 34: taller, and wider still.
//
// A PROPORTIONAL face does fit, at exactly the size that was wanted:
//
//	40px (goregular via FontsWith)  298 + 87 = 385  fits
//
// If a headline needs to be big AND long, that is the lever — not this ladder.
// [FontsWith] keeps the bitmap faces for small text, where an outline genuinely
// cannot compete, and takes yours above 17px.
//
// # What is still not available
//
// Anything between the rungs. Half-step scaling is out at any price: a 1.5x
// face puts glyph edges between pixels, which is the whole reason these are
// integer-scaled bitmaps in the first place.
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
// # Prefer asking the family, in drawing code
//
// This function names THIS family's ladder, so calling it from code that draws
// hardcodes this family into that code — even where the family arrives as a
// parameter and the code claims to work with any. A consumer pointed that out
// in issue #4, having gone looking for a use for this and found a better one.
//
// [render.FontFamily] requires an implementation to round down, so asking for
// the height you have already gets you the largest face that fits:
//
//	f, err := ff(band.Dy() - padding)   // no ladder knowledge needed
//
// That is the right call in a layout. It also degrades sensibly on a panel the
// layout was never designed for, which a hardcoded size does not.
//
// # Where this does belong
//
// Tests, and anything inspecting the family rather than drawing with it.
// Knowing the ladder is exactly the point when the assertion is about the
// ladder — "this headline uses 34 and the next rung up genuinely cannot fit
// beside the clock" is a regression test, and it starts failing the day the
// ladder gains a rung between them. Which is the outcome the reporter wanted.
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

// basicfontHeight is the line height of [basicfont.Face7x13], and so the step
// between the rungs it contributes: 13, 26, 39, 52, 65.
const basicfontHeight = 13

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
//
// Like [Fonts], it honours [render.FontFamily]'s contract: the face it returns
// is never TALLER than the size asked for. That needs saying because it is not
// what a naive implementation does. An outline face built at Size=N has a line
// height of roughly 1.17*N — ascent plus descent exceeds the em — so asking
// opentype for the requested number and handing it back overshoots at every
// size. This one searches down for the largest point size whose LINE HEIGHT
// fits, which is the number a layout is actually working with.
//
// It used to overshoot, by up to 13px at the sizes a headline uses. Nothing in
// this repo noticed, because the card sizes its own text with TextFitted,
// which measures what it is given and defends itself. A consumer leaning on
// the contract to size text without knowing the ladder found it (issue #4).
func FontsWith(ttf []byte) (render.FontFamily, error) {
	parsed, err := opentype.Parse(ttf)
	if err != nil {
		return nil, fmt.Errorf("testcard: parsing font: %w", err)
	}
	bitmap := Fonts()

	var mu sync.Mutex
	cache := map[int]font.Face{}

	// newFace builds the outline face at a given point size. DPI 72 makes one
	// point equal one pixel. Hinting is off: above the bitmap ceiling it
	// changes little, and x/image's hinting distorts glyphs more than it helps.
	newFace := func(points int) (font.Face, error) {
		f, err := opentype.NewFace(parsed, &opentype.FaceOptions{
			Size: float64(points), DPI: 72, Hinting: font.HintingNone,
		})
		if err != nil {
			return nil, fmt.Errorf("testcard: face at %dpx: %w", points, err)
		}
		return f, nil
	}

	return func(size int) (font.Face, error) {
		if size < bitmapCeiling {
			return bitmap(size)
		}

		mu.Lock()
		defer mu.Unlock()
		if f, ok := cache[size]; ok {
			return f, nil
		}

		// Largest point size whose line height fits in the pixels asked for.
		// Walking down from the request is at most a handful of steps, since
		// the overshoot is proportional and small; a closed-form guess would
		// have to trust the ratio, and a font is free to have whatever metrics
		// it likes.
		for points := size; points >= 1; points-- {
			f, err := newFace(points)
			if err != nil {
				return nil, err
			}
			if render.LineHeight(f) <= size {
				cache[size] = f
				return f, nil
			}
		}

		// Every point size is too tall for the box, which means the font has
		// extraordinary metrics. Fall back to the bitmap family rather than
		// returning something that will overflow: too small is recoverable,
		// too big silently draws over the next element.
		f, err := bitmap(size)
		if err != nil {
			return nil, err
		}
		cache[size] = f
		return f, nil
	}, nil
}

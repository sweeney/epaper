package render

import (
	"fmt"
	"image"
	"strings"

	"github.com/sweeney/epaper"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// alphaThreshold is the glyph coverage at which a pixel becomes ink.
//
// The panel has four inks and no intermediate tones, so text is binary: a
// pixel is either ink or it is not. Letting antialiased edges map to "nearest
// palette colour" instead would scatter yellow and red around black text.
//
// The value is a THIRD, not a half, and this matters more than it sounds.
// Rounding at 50% is the intuitive choice and it is wrong here: at 8-12px a
// stem is about one pixel wide and rarely lands on a pixel boundary, so a
// stem covering 40% of every pixel it touches disappears completely. The
// result is text with strokes missing at random — which reads as bad spacing
// rather than as missing ink, because the eye sees the gaps, not the cause.
//
// Chosen by rendering the same text across thresholds and looking at it. At a
// third, 10px text is comfortably legible and 8px is readable; at a half,
// neither is. Larger text is unaffected: its stems cover whole pixels either
// way.
const alphaThreshold = 0x5555

// FontFamily produces a face at a requested pixel size. It is what
// [Canvas.TextFitted] shrinks through.
//
// TextFitted may ask for many sizes in one call, so an implementation that
// parses or allocates should cache. Faces are not closed by this package:
// their lifetime belongs to whoever made them, and closing a cached face would
// break the next caller.
//
// # Use a BITMAP font for small text
//
// This is the single most important thing to get right, and it was learned
// the hard way on the panel.
//
// A scaled outline font cannot render small text well on a display with no
// intermediate tones. Below roughly 16px a stem is about one pixel wide and
// lands at an arbitrary sub-pixel position, so after thresholding some stems
// come out one pixel wide and their neighbours two. Stroke weights vary
// letter to letter, curves blob, and the result reads as bad spacing even
// though the advances are correct. No choice of threshold fixes it: raise it
// and strokes vanish, lower it and they double.
//
// Hinting is supposed to solve exactly this by snapping stems to the pixel
// grid, and FreeType does it well — which is why the same text looks fine
// from Python. x/image's hinting does not, at these sizes.
//
// A bitmap font has no such problem: every glyph was drawn on the pixel grid
// by hand. golang.org/x/image ships three, and any of them beats a scaled
// outline at small sizes:
//
//	basicfont.Face7x13
//	inconsolata.Regular8x16
//	inconsolata.Bold8x16
//
// Outline fonts are fine above ~16px, where stems cover whole pixels anyway.
// A good [FontFamily] therefore returns bitmap faces for small sizes and
// scales an outline above them.
type FontFamily func(sizePx int) (font.Face, error)

// Text draws a single line of text with its top-left corner at p.
//
// Note "top-left", not the baseline. Font APIs normally position text by its
// baseline, which is correct and consistently surprising; since almost every
// e-ink layout is really placing a box, this takes the corner and works the
// baseline out from the face's ascent.
//
// Glyph coverage is thresholded rather than blended — see the package docs on
// why a four-ink panel wants binary text. Text is clipped to the canvas, which
// on e-ink means it silently vanishes; use [Canvas.TextFitted] or
// [MeasureText] rather than assuming a string fits.
func (c *Canvas) Text(p image.Point, s string, f font.Face, i epaper.Ink) {
	idx, ok := c.ink(i)
	if !ok {
		return
	}
	if f == nil {
		c.fail(fmt.Errorf("render: text: font face is nil"))
		return
	}

	dot := fixed.Point26_6{
		X: fixed.I(p.X),
		Y: fixed.I(p.Y) + f.Metrics().Ascent,
	}
	prev := rune(-1)
	for _, r := range s {
		if prev >= 0 {
			dot.X += f.Kern(prev, r)
		}
		dr, mask, maskp, advance, ok := f.Glyph(dot, r)
		if !ok {
			// No glyph for this rune. Advance anyway so the rest of the
			// string keeps its position rather than sliding left.
			a, _ := f.GlyphAdvance(r)
			dot.X += a
			prev = r
			continue
		}
		c.blit(dr, mask, maskp, idx)
		dot.X += advance
		prev = r
	}
}

// blit paints a glyph mask, thresholded to a single ink.
func (c *Canvas) blit(dr image.Rectangle, mask image.Image, maskp image.Point, idx uint8) {
	clipped := dr.Intersect(c.img.Rect)
	for y := clipped.Min.Y; y < clipped.Max.Y; y++ {
		for x := clipped.Min.X; x < clipped.Max.X; x++ {
			mx := maskp.X + (x - dr.Min.X)
			my := maskp.Y + (y - dr.Min.Y)
			if _, _, _, a := mask.At(mx, my).RGBA(); a >= alphaThreshold {
				c.img.Pix[c.img.PixOffset(x, y)] = idx
			}
		}
	}
}

// TextFitted draws text as large as will fit inside r, and returns the pixel
// size it used. It returns 0, and records an error, if the string will not fit
// at any size.
//
// This exists because **every** layout bug found on the bench was silent
// clipping: a header losing its final letter, a row cut mid-word, a footer
// drawn over a pattern. On e-ink there is no scrollbar and no overflow
// indicator — text that does not fit simply is not there, and nobody is
// watching the panel at the moment it happens. Asking for the largest size
// that fits makes that failure loud at the point it occurs.
//
// Sizes are tried from the height of r downwards, so the result is the largest
// that fits both the width and the line height.
func (c *Canvas) TextFitted(r image.Rectangle, s string, ff FontFamily, i epaper.Ink) int {
	if _, ok := c.ink(i); !ok {
		return 0
	}
	if ff == nil {
		c.fail(fmt.Errorf("render: text fitted: font family is nil"))
		return 0
	}
	if r.Empty() {
		c.fail(fmt.Errorf("render: text fitted: rectangle %v is empty", r))
		return 0
	}

	size, face, err := FittedSize(r, s, ff)
	if err != nil {
		c.fail(fmt.Errorf("render: text fitted: %w", err))
		return 0
	}
	c.Text(r.Min, s, face, i)
	return size
}

// FittedSize reports the largest size at which s fits in r, and the face at
// that size, without drawing anything.
//
// It is what [Canvas.TextFitted] uses to choose a size, exposed separately for
// callers that need to know whether text will fit before committing to it.
// TextFitted deliberately fails loudly when nothing fits — silently shrinking
// text to nothing is the failure it exists to prevent — which makes it the
// wrong tool for a layout that wants to leave an element out instead.
//
// The test card uses it for exactly that: a panel can be too small for a
// heading inside the central disc, and on such a panel the right answer is a
// card without a heading, not a card that refuses to draw.
//
// Sizes are tried from the height of r downwards. Note that the bundled
// bitmap faces have a minimum line height of about 13px, so a box shorter than
// that fits no text at any size — which is a property of the fonts, not a bug
// here.
func FittedSize(r image.Rectangle, s string, ff FontFamily) (int, font.Face, error) {
	if ff == nil {
		return 0, nil, fmt.Errorf("font family is nil")
	}
	if r.Empty() {
		return 0, nil, fmt.Errorf("rectangle %v is empty", r)
	}
	for size := r.Dy(); size >= 1; size-- {
		face, err := ff(size)
		if err != nil {
			return 0, nil, fmt.Errorf("font family at %dpx: %w", size, err)
		}
		if MeasureText(s, face) <= r.Dx() && LineHeight(face) <= r.Dy() {
			return size, face, nil
		}
	}
	return 0, nil, fmt.Errorf("%q does not fit in %v at any size: %w", s, r, ErrTextDoesNotFit)
}

// MeasureText returns the advance width of a string in pixels, kerning
// included.
//
// The result is rounded **up**. A measurement that under-reports by a fraction
// of a pixel is how a layout ends up one character short with no indication
// that anything went wrong.
func MeasureText(s string, f font.Face) int {
	if f == nil {
		return 0
	}
	return ceilFixed(font.MeasureString(f, s))
}

// LineHeight returns a face's ascent plus descent in pixels, rounded up. This
// is the height a single line of text occupies.
func LineHeight(f font.Face) int {
	if f == nil {
		return 0
	}
	m := f.Metrics()
	return ceilFixed(m.Ascent + m.Descent)
}

// ceilFixed converts a 26.6 fixed-point value to whole pixels, rounding up.
func ceilFixed(v fixed.Int26_6) int {
	return int((v + 0x3F) >> 6)
}

// WrapText breaks a string into lines that each fit within width pixels.
//
// Breaks happen at spaces. A single word wider than the line is left whole on
// a line of its own rather than being cut: a truncated word is usually worse
// than a wide one, and [Canvas.TextWrapped] will report the overflow anyway.
//
// Runs of whitespace collapse, and a string of only whitespace returns nil.
func WrapText(s string, f font.Face, width int) []string {
	if f == nil {
		return nil
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}

	var lines []string
	cur := words[0]
	for _, word := range words[1:] {
		candidate := cur + " " + word
		if MeasureText(candidate, f) <= width {
			cur = candidate
			continue
		}
		lines = append(lines, cur)
		cur = word
	}
	return append(lines, cur)
}

// TextWrapped draws a string into a rectangle, breaking it across lines, and
// returns the number of lines it drew.
//
// Lines that would fall outside the rectangle are not drawn, and that records
// [ErrTextDoesNotFit] on the canvas. This is the same bargain the rest of the
// package makes: the text is clipped rather than spilling over your layout,
// but you are told, because on e-ink nobody is watching at the moment it
// happens.
//
// There are three answers to "the text does not fit", and they differ in what
// gives: [Canvas.TextFitted] gives the size, this gives the line count, and
// [Canvas.TextTruncated] gives the words. A dashboard usually wants the last —
// text that resizes itself between refreshes makes a screen look broken.
func (c *Canvas) TextWrapped(r image.Rectangle, s string, f font.Face, i epaper.Ink) int {
	if _, ok := c.ink(i); !ok {
		return 0
	}
	if f == nil {
		c.fail(fmt.Errorf("render: text wrapped: font face is nil"))
		return 0
	}

	lines := WrapText(s, f, r.Dx())
	lh := LineHeight(f)
	if lh <= 0 {
		return 0
	}

	drawn := 0
	y := r.Min.Y
	for _, line := range lines {
		if y+lh > r.Max.Y {
			break
		}
		c.Text(image.Pt(r.Min.X, y), line, f, i)
		y += lh
		drawn++
	}

	if drawn < len(lines) {
		c.fail(fmt.Errorf("render: text wrapped: %d of %d lines did not fit in %v: %w",
			len(lines)-drawn, len(lines), r, ErrTextDoesNotFit))
	}
	return drawn
}

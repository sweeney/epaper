package render

import (
	"fmt"
	"image"

	"github.com/sweeney/epaper"
)

// bayer4 is the 4x4 ordered-dither matrix, indexed [y][x].
//
// Both details matter and both are easy to get wrong in a way that still looks
// like a plausible ramp: the matrix is indexed row-first, and the threshold
// adds 0.5 before dividing by 16. The conformance fixture pins them.
var bayer4 = [4][4]int{
	{0, 8, 2, 10},
	{12, 4, 14, 6},
	{3, 11, 1, 9},
	{15, 7, 13, 5},
}

// ditherSteps is how many distinct tones the 4x4 matrix can express, not
// counting the all-background end: sixteen cells, so sixteen thresholds.
const ditherSteps = 16

// DitherLevels returns every tone [Canvas.Dither] can actually produce:
// 0, 1/16, 2/16, ... 1. Seventeen of them, ascending.
//
// It exists because Dither's ratio is a float64 and therefore reads like a
// knob you can turn to any position. It is not. The 4x4 matrix quantises it,
// so a caller who types 0.18, looks at the panel, tries 0.20 and sees no
// change cannot tell whether the change was too small or simply had nowhere to
// land. A consumer spent that afternoon (issue #5); it is the same trap as the
// font ladder in issue #4, which is a discrete set behind a continuous-looking
// interface, and it has the same answer — name the rungs.
//
// The k-th level inks EXACTLY k sixteenths of the area, so the number is also
// the answer: 0.25 means a quarter of the pixels, not approximately a quarter.
// That is what makes this a list worth choosing from rather than a hint.
//
// Use [NearestDitherLevel] to find which rung a ratio you already have lands
// on.
func DitherLevels() []float64 {
	out := make([]float64, ditherSteps+1)
	for k := range out {
		out[k] = float64(k) / ditherSteps
	}
	return out
}

// NearestDitherLevel snaps a ratio to the level [Canvas.Dither] will actually
// draw it as, clamped to 0..1.
//
// Drawing with the result is identical to drawing with the input — that is the
// point of it. It answers "which of the seventeen did I just pick?", which is
// the question a caller has after choosing a number by eye, and it makes a
// chosen tone something you can write down and compare rather than a magic
// constant nobody dares touch.
func NearestDitherLevel(ratio float64) float64 {
	switch {
	case ratio <= 0:
		return 0
	case ratio >= 1:
		return 1
	}
	// Round to the nearest sixteenth. Dither inks a pixel when the ratio
	// exceeds (b+0.5)/16, so the band around k/16 runs from (k-0.5)/16 to
	// (k+0.5)/16 — which is exactly what rounding does.
	k := int(ratio*ditherSteps + 0.5)
	return float64(k) / ditherSteps
}

// Dither fills a rectangle with an ordered mix of two inks, approximating a
// tone between them.
//
// ratio is the proportion of ink to background, 0 to 1. Values outside that
// range are not an error: they simply come out entirely background or entirely
// ink.
//
// THE RATIO IS QUANTISED. There are seventeen distinct results and no more:
// see [DitherLevels] for the list and [NearestDitherLevel] to find which one a
// given number lands on. Passing 0.18 and 0.20 draws the same picture.
//
// A pixel takes the ink when
//
//	ratio > (bayer4[y%4][x%4] + 0.5) / 16
//
// Note that x and y are canvas coordinates, not offsets into the rectangle, so
// two dithered rectangles at the same ratio tile seamlessly instead of showing
// a seam where they meet.
//
// This is how a four-ink panel gets more than four tones, and on this hardware
// it genuinely works: 1px detail resolves cleanly, which is why the panel's
// strength is crisp pattern rather than photographs.
func (c *Canvas) Dither(r image.Rectangle, ink, bg epaper.Ink, ratio float64) {
	inkIdx, ok := c.ink(ink)
	if !ok {
		return
	}
	bgIdx, ok := c.ink(bg)
	if !ok {
		return
	}
	r = r.Intersect(c.img.Rect)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			threshold := (float64(bayer4[mod4(y)][mod4(x)]) + 0.5) / 16.0
			idx := bgIdx
			if ratio > threshold {
				idx = inkIdx
			}
			c.img.Pix[c.img.PixOffset(x, y)] = idx
		}
	}
}

// Checker fills a rectangle with a checkerboard of two inks.
//
// cell is the square size in pixels and must be at least 1. As with
// [Canvas.Dither], the grid is anchored to the canvas rather than to the
// rectangle, so adjacent patches line up.
//
// At cell size 1 this is a 50% mix of the two inks, and on this panel it
// resolves cleanly — a yellow/red 1px checker reads as orange from a normal
// viewing distance.
func (c *Canvas) Checker(r image.Rectangle, a, b epaper.Ink, cell int) {
	aIdx, ok := c.ink(a)
	if !ok {
		return
	}
	bIdx, ok := c.ink(b)
	if !ok {
		return
	}
	if cell < 1 {
		c.fail(fmt.Errorf("render: checker cell size must be at least 1, got %d", cell))
		return
	}
	r = r.Intersect(c.img.Rect)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			idx := bIdx
			if (floorDiv(x, cell)+floorDiv(y, cell))%2 == 0 {
				idx = aIdx
			}
			c.img.Pix[c.img.PixOffset(x, y)] = idx
		}
	}
}

// mod4 is x%4 for possibly-negative x, so the matrix tiles continuously across
// the origin instead of mirroring at it.
func mod4(v int) int {
	return ((v % 4) + 4) % 4
}

// floorDiv rounds towards negative infinity, so the checker grid stays regular
// across the origin. Go's / truncates towards zero, which would make the cell
// at x = -1 the same one as at x = 0.
func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

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

// Dither fills a rectangle with an ordered mix of two inks, approximating a
// tone between them.
//
// ratio is the proportion of ink to background, 0 to 1. Values outside that
// range are not an error: they simply come out entirely background or entirely
// ink.
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

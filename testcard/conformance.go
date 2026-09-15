package testcard

// This file holds the conformance pattern; the package doc is in testcard.go.

import (
	"image"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/render"
)

// DrawConformance paints the pattern that testdata/conformance pins.
//
// Unlike [Draw] it contains no text, which is what makes it byte-comparable
// against the vendor library's output: Pillow and x/image rasterise glyphs
// differently, and no text-bearing image can ever match across the two. Every
// shape follows an integer rule written down in testdata/README.md.
//
// Use it to check a port, or to check wiring: the four single-pixel corner
// markers catch flips and transpositions that leave the rest of the pattern
// looking entirely plausible.
func DrawConformance(c *render.Canvas) {
	const w, h = 400, 300

	// The generator starts from a white buffer; a Go canvas starts at index 0,
	// which is black. Bands 2 and 6 only paint their marks, not their ground.
	c.Fill(epaper.White)

	// band 1 (y 0..39): flat quadrants of all four inks
	for i, ink := range []epaper.Ink{epaper.Black, epaper.White, epaper.Yellow, epaper.Red} {
		c.Rect(image.Rect(i*100, 0, i*100+100, 40), ink)
	}

	// band 2 (y 40..99): 1px horizontal rules at pitch 2,3,4,5
	for i, pitch := range []int{2, 3, 4, 5} {
		x0 := i * 100
		for y := 40; y < 100; y++ {
			if (y-40)%pitch == 0 {
				c.Rect(image.Rect(x0, y, x0+100, y+1), epaper.Black)
			}
		}
	}

	// band 3 (y 100..159): checkerboards at cell 1, 2, 4, plus yellow/red
	for i, ck := range []struct {
		cell int
		a, b epaper.Ink
	}{
		{1, epaper.Black, epaper.White},
		{2, epaper.Black, epaper.White},
		{4, epaper.Black, epaper.White},
		{1, epaper.Yellow, epaper.Red},
	} {
		x0 := i * 100
		c.Checker(image.Rect(x0, 100, x0+100, 160), ck.a, ck.b, ck.cell)
	}

	// bands 4 and 5 (y 160..259): Bayer ramps, ratio 0.0 -> 1.0 left to right
	for _, band := range []struct {
		y0, y1  int
		ink, bg epaper.Ink
	}{
		{160, 220, epaper.Black, epaper.White},
		{220, 240, epaper.Yellow, epaper.White},
		{240, 260, epaper.Red, epaper.Black},
	} {
		for x := range w {
			ratio := float64(x) / float64(w-1)
			c.Dither(image.Rect(x, band.y0, x+1, band.y1), band.ink, band.bg, ratio)
		}
	}

	// band 6 (y 260..299): geometry primitives. Rectangles are half-open, so
	// the fixture's inclusive (4,264)-(96,295) is Rect(4,264,97,296).
	c.Rect(image.Rect(4, 264, 97, 296), epaper.Red)
	c.StrokeRect(image.Rect(4, 264, 97, 296), epaper.Black)
	c.Ellipse(image.Rect(104, 264, 165, 296), epaper.Yellow)
	c.StrokeEllipse(image.Rect(104, 264, 165, 296), epaper.Black)
	c.Line(image.Pt(172, 295), image.Pt(260, 264), epaper.Black)
	c.Line(image.Pt(172, 264), image.Pt(260, 295), epaper.Red)
	c.Polygon([]image.Point{{272, 295}, {316, 264}, {360, 295}}, epaper.Black)

	// single-pixel corner markers: these catch off-by-one, row/column
	// transposition and flips, all of which otherwise produce an image that
	// looks broadly plausible.
	c.Set(396, 296, epaper.Red)
	c.Set(399, 299, epaper.Black)
	c.Set(396, 299, epaper.Yellow)
	c.Set(399, 296, epaper.White)

	_ = h
}

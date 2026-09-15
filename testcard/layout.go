package testcard

// The diagnostic elements around the outside of the card: the parts that
// measure something. The central disc is in circle.go.

import (
	"image"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/render"
)

// drawCastellation paints alternating blocks around all four edges. Any
// missing or doubled block means a geometry error at the very edge of the
// panel, which is where off-by-ones live.
func drawCastellation(c *render.Canvas, w, h int) {
	for i, x := 0, 0; x < w; i, x = i+1, x+castle {
		ink := alternate(i)
		c.Rect(image.Rect(x, 0, x+castle, border), ink)
		c.Rect(image.Rect(x, h-border, x+castle, h), ink)
	}
	for i, y := 0, 0; y < h; i, y = i+1, y+castle {
		ink := alternate(i)
		c.Rect(image.Rect(0, y, border, y+castle), ink)
		c.Rect(image.Rect(w-border, y, w, y+castle), ink)
	}
}

// ladder is one step of the luminance ramp, lightest first.
type ladder struct {
	name  string
	paint func(c *render.Canvas, r image.Rectangle)
}

// ladderSteps runs light to dark. Only four of the seven are real inks; the
// rest are mixes, which is the point.
var ladderSteps = []ladder{
	{"white", func(c *render.Canvas, r image.Rectangle) { c.Rect(r, epaper.White) }},
	{"yellow", func(c *render.Canvas, r image.Rectangle) { c.Rect(r, epaper.Yellow) }},
	{"orange", func(c *render.Canvas, r image.Rectangle) { c.Checker(r, epaper.Yellow, epaper.Red, 1) }},
	{"red", func(c *render.Canvas, r image.Rectangle) { c.Rect(r, epaper.Red) }},
	{"olive", func(c *render.Canvas, r image.Rectangle) { c.Dither(r, epaper.Yellow, epaper.Black, 0.5) }},
	{"dark red", func(c *render.Canvas, r image.Rectangle) { c.Dither(r, epaper.Red, epaper.Black, 0.5) }},
	{"black", func(c *render.Canvas, r image.Rectangle) { c.Rect(r, epaper.Black) }},
}

func drawLadders(c *render.Canvas, w, h int) {
	const barW = 23
	top, bottom := 66, h-66
	seg := (bottom - top) / len(ladderSteps)

	for _, x0 := range []int{border, w - border - barW} {
		for i, step := range ladderSteps {
			y0 := top + i*seg
			step.paint(c, image.Rect(x0, y0, x0+barW, y0+seg))
		}
		c.StrokeRect(image.Rect(x0, top, x0+barW, top+seg*len(ladderSteps)), epaper.Black)
	}
}

// drawGratings paints the frequency wedges. If the panel cannot resolve a
// pitch, that block turns into flat grey rather than stripes — which is
// exactly the measurement.
func drawGratings(c *render.Canvas, w, h int) {
	type grating struct {
		r     image.Rectangle
		pitch int
		kind  string
		diag  int
	}
	for _, g := range []grating{
		{image.Rect(40, 20, 92, 58), 3, "hatch", 1},
		{image.Rect(w-92, 20, w-40, 58), 3, "hatch", -1},
		{image.Rect(40, h-70, 92, h-32), 4, "bars", 0},
		{image.Rect(w-92, h-70, w-40, h-32), 6, "bars", 0},
	} {
		if g.kind == "hatch" {
			hatch(c, g.r, g.pitch, g.diag)
		} else {
			bars(c, g.r, g.pitch)
		}
		c.StrokeRect(g.r, epaper.Black)
	}
}

// hatch draws a diagonal grating, the original's resolution wedge.
func hatch(c *render.Canvas, r image.Rectangle, pitch, diag int) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			ink := epaper.White
			if mod(x+diag*y, pitch) == 0 {
				ink = epaper.Black
			}
			c.Set(x, y, ink)
		}
	}
}

// bars draws a vertical grating.
func bars(c *render.Canvas, r image.Rectangle, pitch int) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			ink := epaper.White
			if (x-r.Min.X)%pitch < pitch/2 {
				ink = epaper.Black
			}
			c.Set(x, y, ink)
		}
	}
}

// drawReferencePatch is pure black on pure white, with no dithering anywhere
// near it: the reference both ends of the ladder are judged against.
func drawReferencePatch(c *render.Canvas, w int) {
	mid := w / 2
	outer := image.Rect(mid-50, 22, mid+50, 53)
	c.Rect(outer, epaper.White)
	c.StrokeRect(outer, epaper.Black)
	c.Rect(image.Rect(mid-40, 30, mid+40, 45), epaper.Black)
}

// drawWedges paints the two step wedges: the thing dithering exists to prove.
//
// Each sits on a white plaque. At 50% the field is itself a checkerboard, so a
// wedge laid straight onto it loses its middle steps entirely — which the
// Python original discovered the hard way.
func drawWedges(c *render.Canvas, w int) {
	const steps = 6
	y0, y1 := 94, 202
	sh := (y1 - y0) / steps

	// Left: black into white, a true tonal ramp.
	lx0, lx1 := 74, 104
	plaque(c, image.Rect(lx0, y0, lx1, y0+steps*sh))
	for i := range steps {
		r := float64(i) / float64(steps-1)
		c.Dither(image.Rect(lx0, y0+i*sh, lx1, y0+(i+1)*sh), epaper.Black, epaper.White, r)
	}
	c.StrokeRect(image.Rect(lx0, y0, lx1, y0+steps*sh), epaper.Black)

	// Right: the same idea through the accent inks, light to dark.
	rx0, rx1 := w-104, w-74
	plaque(c, image.Rect(rx0, y0, rx1, y0+steps*sh))
	mixes := []struct {
		ink, bg epaper.Ink
		ratio   float64
		checker bool
	}{
		{epaper.Yellow, epaper.White, 0.5, false},
		{epaper.Yellow, epaper.White, 1.0, false},
		{epaper.Yellow, epaper.Red, 0, true},
		{epaper.Red, epaper.White, 1.0, false},
		{epaper.Red, epaper.Black, 0.5, false},
		{epaper.Black, epaper.White, 1.0, false},
	}
	for i, m := range mixes {
		box := image.Rect(rx0, y0+i*sh, rx1, y0+(i+1)*sh)
		if m.checker {
			c.Checker(box, m.ink, m.bg, 1)
		} else {
			c.Dither(box, m.ink, m.bg, m.ratio)
		}
	}
	c.StrokeRect(image.Rect(rx0, y0, rx1, y0+steps*sh), epaper.Black)
}

func plaque(c *render.Canvas, r image.Rectangle) {
	outer := r.Inset(-4)
	c.Rect(outer, epaper.White)
	c.StrokeRect(outer, epaper.Black)
}

// drawCrosshair marks the exact centre lines at an 8px pitch. If the panel is
// flipped or transposed, the ticks stop meeting in the middle.
//
// The ticks sit on a white rule. Drawn straight onto the 50% dithered field
// they were invisible — half of every tick landed on a field pixel that was
// already black. Found by looking at the golden, which is what goldens are
// for.
func drawCrosshair(c *render.Canvas, w, h int) {
	cx, cy := w/2, h/2
	c.Rect(image.Rect(border, cy, w-border, cy+1), epaper.White)
	c.Rect(image.Rect(cx, border, cx+1, h-border), epaper.White)
	for x := border; x < w-border; x += 8 {
		c.Set(x, cy, epaper.Black)
	}
	for y := border; y < h-border; y += 8 {
		c.Set(cx, y, epaper.Black)
	}
}

func alternate(i int) epaper.Ink {
	if i%2 == 0 {
		return epaper.Black
	}
	return epaper.White
}

func mod(a, b int) int { return ((a % b) + b) % b }

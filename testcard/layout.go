package testcard

// The diagnostic elements around the outside of the card: the parts that
// measure something. The central disc is in circle.go, and the geometry they
// are all positioned from is in geometry.go.

import (
	"image"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/render"
)

// drawCastellation paints alternating blocks around all four edges. Any
// missing or doubled block means a geometry error at the very edge of the
// panel, which is where off-by-ones live.
func drawCastellation(c *render.Canvas, l layout) {
	for i, x := 0, 0; x < l.w; i, x = i+1, x+l.castle {
		ink := alternate(i)
		c.Rect(image.Rect(x, 0, x+l.castle, l.border), ink)
		c.Rect(image.Rect(x, l.h-l.border, x+l.castle, l.h), ink)
	}
	for i, y := 0, 0; y < l.h; i, y = i+1, y+l.castle {
		ink := alternate(i)
		c.Rect(image.Rect(0, y, l.border, y+l.castle), ink)
		c.Rect(image.Rect(l.w-l.border, y, l.w, y+l.castle), ink)
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

func drawLadders(c *render.Canvas, l layout) {
	seg := (l.ladderBottom - l.ladderTop) / len(ladderSteps)
	if seg < 1 {
		// No room for a seven-step ramp. Drawing it anyway inverts every
		// rectangle, which is how this looked on a 122px panel before the
		// layout scaled: seven overlapping bars in the wrong order.
		return
	}

	for _, x0 := range []int{l.border, l.w - l.border - l.ladderBarW} {
		for i, step := range ladderSteps {
			y0 := l.ladderTop + i*seg
			step.paint(c, image.Rect(x0, y0, x0+l.ladderBarW, y0+seg))
		}
		c.StrokeRect(image.Rect(x0, l.ladderTop, x0+l.ladderBarW, l.ladderTop+seg*len(ladderSteps)), epaper.Black)
	}
}

// drawGratings paints the frequency wedges. If the panel cannot resolve a
// pitch, that block turns into flat grey rather than stripes — which is
// exactly the measurement.
//
// The pitches are deliberately absolute, not scaled: a 3px grating is the same
// test on every panel, and scaling it to 1px on a small one would quietly make
// it a much harder one.
func drawGratings(c *render.Canvas, l layout) {
	type grating struct {
		r     image.Rectangle
		pitch int
		kind  string
		diag  int
	}
	top := image.Rect(l.gratingInset, l.gratingTop, l.gratingInset+l.gratingW, l.gratingTop+l.gratingH)
	bot := image.Rect(l.gratingInset, l.h-l.gratingBottom, l.gratingInset+l.gratingW, l.h-l.gratingBottom+l.gratingH)
	mirror := func(r image.Rectangle) image.Rectangle {
		return image.Rect(l.w-r.Max.X, r.Min.Y, l.w-r.Min.X, r.Max.Y)
	}

	for _, g := range []grating{
		{top, 3, "hatch", 1},
		{mirror(top), 3, "hatch", -1},
		{bot, 4, "bars", 0},
		{mirror(bot), 6, "bars", 0},
	} {
		if g.r.Empty() {
			continue
		}
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
func drawReferencePatch(c *render.Canvas, l layout) {
	mid := l.w / 2
	outer := image.Rect(mid-l.patchHalfW, l.patchTop, mid+l.patchHalfW, l.patchBottom)
	if outer.Empty() {
		return
	}
	c.Rect(outer, epaper.White)
	c.StrokeRect(outer, epaper.Black)
	c.Rect(image.Rect(mid-l.patchInnerHalfW, l.patchInnerTop, mid+l.patchInnerHalfW, l.patchInnerBot), epaper.Black)
}

// drawWedges paints the two step wedges: the thing dithering exists to prove.
//
// Each sits on a white plaque. At 50% the field is itself a checkerboard, so a
// wedge laid straight onto it loses its middle steps entirely — which the
// Python original discovered the hard way.
func drawWedges(c *render.Canvas, l layout) {
	const steps = 6
	sh := (l.wedgeBottom - l.wedgeTop) / steps
	if sh < 1 {
		return // no room; see drawLadders
	}
	y0 := l.wedgeTop

	// Left: black into white, a true tonal ramp.
	lx0, lx1 := l.wedgeInset, l.wedgeInset+l.wedgeW
	plaque(c, image.Rect(lx0, y0, lx1, y0+steps*sh))
	for i := range steps {
		r := float64(i) / float64(steps-1)
		c.Dither(image.Rect(lx0, y0+i*sh, lx1, y0+(i+1)*sh), epaper.Black, epaper.White, r)
	}
	c.StrokeRect(image.Rect(lx0, y0, lx1, y0+steps*sh), epaper.Black)

	// Right: the same idea through the accent inks, light to dark.
	rx0, rx1 := l.w-lx1, l.w-lx0
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

// drawCrosshair marks the exact centre lines at a fixed pitch. If the panel is
// flipped or transposed, the ticks stop meeting in the middle.
//
// The ticks sit on a white rule. Drawn straight onto the 50% dithered field
// they were invisible — half of every tick landed on a field pixel that was
// already black. Found by looking at the golden, which is what goldens are
// for.
func drawCrosshair(c *render.Canvas, l layout) {
	cx, cy := l.w/2, l.h/2
	c.Rect(image.Rect(l.border, cy, l.w-l.border, cy+1), epaper.White)
	c.Rect(image.Rect(cx, l.border, cx+1, l.h-l.border), epaper.White)
	for x := l.border; x < l.w-l.border; x += l.crossPitch {
		c.Set(x, cy, epaper.Black)
	}
	for y := l.border; y < l.h-l.border; y += l.crossPitch {
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

// Package testcard draws a four-ink test card for e-ink panels.
//
// It is the visual acceptance test: a pattern designed so that a person
// looking at the panel can tell, in a few seconds, whether the whole stack
// works — geometry, all four inks, dithering, fine detail and text.
//
// # What this is, and what it is not
//
// It follows BBC Test Card F in structure: castellated border, luminance
// ladders down both sides, frequency gratings in the corners, step wedges, a
// crosshair and a central circle. Those are the parts that measure something.
//
// It does NOT reproduce the card's picture — the girl, the blackboard, the
// clown. Those need arcs and pie slices, which [render.Canvas] deliberately
// does not offer, and they test nothing that the diagnostic elements do not
// already cover. The central circle carries a legibility ladder and a pixel
// grid instead, which are more useful on a 400x300 panel than a drawing is.
//
// # No greys, no cyan, no green
//
// The panel has four inks. Everything between them is made by ordered dither
// and checkerboards, which the bench proved this hardware resolves cleanly at
// 1px:
//
//	grey       4x4 Bayer black-on-white at a ratio
//	orange     1px yellow/red checkerboard
//	olive      yellow dithered into black
//	dark red   red dithered into black
//
// The ladders keep Test Card F's luminance ORDER rather than pretending to
// hues the panel cannot make. That is the honest translation: the card exists
// to check luminance steps and frequency response, and both survive the
// palette loss intact.
package testcard

import (
	"context"
	"fmt"
	"image"
	"strings"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/render"
	"golang.org/x/image/font"
)

// Layout constants. The card is designed for 400x300 and scales nothing; a
// different panel gets the same elements in the same places, clipped.
const (
	border = 12 // castellation thickness
	castle = 25 // castellation block pitch

	// The central disc.
	circleR = 78 // ~52% of the panel height, as the original
	circleX = 200
	circleY = 148
)

// DefaultTitle is the heading drawn in the circle when [Options.Title] is
// empty.
const DefaultTitle = "TEST CARD"

// Options tunes the card. The zero value is valid: it draws the default
// heading using the built-in bitmap fonts, and needs no font file.
type Options struct {
	// Title is the heading inside the circle. Empty means [DefaultTitle].
	Title string

	// Lines are printed under the title, top to bottom. A line too wide for
	// the circle is wrapped on spaces rather than shrunk, because shrinking
	// it would defeat the legibility this card exists to demonstrate.
	//
	// This is where to put whatever identifies the run: the panel model, a
	// timestamp, a build number, the name of the thing being tested. A
	// photograph of the panel then records what produced it.
	Lines []string

	// Fonts supplies the faces. Nil means [Fonts], which needs no font file.
	//
	// Set it to draw the card in your own typeface — but read
	// [render.FontFamily] first, because a scaled outline font cannot render
	// the small labels legibly on a panel with no intermediate tones.
	Fonts render.FontFamily

	// NoText draws the card without any words at all. Every diagnostic
	// element still works; you lose the title, the lines and the legibility
	// ladder, which are the only parts that need a font.
	NoText bool
}

// Show draws the card on a device and refreshes it.
//
// This is the one-liner for proving a panel: if what appears looks like the
// card, then the wiring, the transport, the command sequence, all four inks,
// the dithering and the text rendering are all working. A full refresh takes
// around 20 seconds.
//
// The device's model name is printed first, followed by any lines given.
//
//	dev, err := inky.Open()
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer dev.Close()
//
//	if err := testcard.Show(ctx, dev, time.Now().Format(time.RFC3339)); err != nil {
//		log.Fatal(err)
//	}
func Show(ctx context.Context, d epaper.Device, lines ...string) error {
	c := render.NewCanvasFor(d)
	Draw(c, Options{Lines: append([]string{d.Model()}, lines...)})
	if err := c.Err(); err != nil {
		return fmt.Errorf("testcard: drawing: %w", err)
	}
	return d.Show(ctx, c.Image())
}

// Draw paints the test card into a canvas.
//
// It is designed for 400x300 and scales nothing: a larger panel gets the same
// elements in the same places, and a smaller one gets them clipped.
func Draw(c *render.Canvas, opts Options) {
	fonts := opts.Fonts
	if fonts == nil {
		fonts = Fonts()
	}
	if opts.NoText {
		fonts = nil
	}
	title := opts.Title
	if title == "" {
		title = DefaultTitle
	}

	b := c.Bounds()
	w, h := b.Dx(), b.Dy()

	// The field: 50% Bayer, which is the "grey" the original card sits on.
	c.Dither(b, epaper.Black, epaper.White, 0.5)

	drawCastellation(c, w, h)
	drawLadders(c, w, h)
	drawGratings(c, w, h)
	drawReferencePatch(c, w)
	drawWedges(c, w)
	drawCrosshair(c, w, h)
	drawCircle(c, fonts, title, opts.Lines)
}

// drawWithoutCircle paints everything except the central disc and its
// contents. It exists so a test can diff the two and prove nothing drawn for
// the circle escapes it.
func drawWithoutCircle(c *render.Canvas) {
	b := c.Bounds()
	w, h := b.Dx(), b.Dy()
	c.Dither(b, epaper.Black, epaper.White, 0.5)
	drawCastellation(c, w, h)
	drawLadders(c, w, h)
	drawGratings(c, w, h)
	drawReferencePatch(c, w)
	drawWedges(c, w)
	drawCrosshair(c, w, h)
}

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

// drawCircle paints the central disc and fills it with the things worth
// checking on a small panel: what it is, whether text is legible, and whether
// single pixels resolve.
//
// Every element is sized from the circle's own geometry rather than from
// guessed constants. A first attempt used fixed widths and every line of text
// ran out through the side of the circle — the exact overflow this library
// exists to make hard, caught by looking at the golden.
func drawCircle(c *render.Canvas, fonts render.FontFamily, title string, lines []string) {
	disc := image.Rect(circleX-circleR, circleY-circleR, circleX+circleR+1, circleY+circleR+1)
	c.Ellipse(disc, epaper.White)
	c.StrokeEllipse(disc, epaper.Black)

	// Everything inside is laid out top to bottom from a running cursor, so
	// adding or resizing an element cannot silently land on top of the next
	// one. A first attempt used fixed offsets and the legibility ladder drew
	// straight over the pixel grid.
	y := circleY - circleR + 16

	if fonts != nil {
		y = fitLine(c, fonts, y, 20, title, epaper.Red)

		// Caller lines. A panel model off an EEPROM can be longer than the
		// circle is wide — "Red/Yellow wHAT (JD79668)" in a monospace bitmap
		// face needs 175px and the circle is 156 at its widest — so these
		// wrap rather than shrink.
		for _, line := range lines {
			if line == "" {
				continue
			}
			y = drawWrapped(c, fonts, y+1, line, epaper.Black)
			if y >= circleBottom() {
				break // the caller gave us more than the circle holds
			}
		}

		// A legibility ladder, smallest last. These are the faces the family
		// actually hands out, so the card shows what the library will really
		// draw at each size.
		for _, size := range []int{16, 13} {
			f, err := fonts(size)
			if err != nil {
				break
			}
			lh := render.LineHeight(f)
			if y+lh > circleBottom() {
				break // out of room; the caller gave us a lot of lines
			}
			half := inscribedHalfWidth(maxAbs(y-circleY, y+lh-circleY))
			text := "Hamburgefonstiv"
			if render.MeasureText(text, f) > 2*half {
				text = "Hamburgef"
			}
			c.Text(image.Pt(circleX-render.MeasureText(text, f)/2, y), text, f, epaper.Black)
			y += lh + 1
		}
		y += 3
	}

	// A 1px grid: if any of it greys out, the panel is not resolving pixels.
	gridH := 22
	if half := inscribedHalfWidth(maxAbs(y-circleY, y+gridH-circleY)); half > 12 {
		c.Checker(image.Rect(circleX-half, y, circleX+half, y+gridH), epaper.Black, epaper.White, 1)
		c.StrokeRect(image.Rect(circleX-half, y, circleX+half, y+gridH), epaper.Black)
		y += gridH + 2
	}

	// Flat accent patches, for judging the inks against the dithered mixes
	// in the ladders either side of the card.
	patchH := 14
	if half := inscribedHalfWidth(maxAbs(y-circleY, y+patchH-circleY)); half > 12 {
		c.Checker(image.Rect(circleX-half, y, circleX, y+patchH), epaper.Yellow, epaper.Red, 1)
		c.Rect(image.Rect(circleX+1, y, circleX+half, y+patchH), epaper.Red)
		c.StrokeRect(image.Rect(circleX-half, y, circleX+half, y+patchH), epaper.Black)
	}
}

// drawWrapped draws a string inside the circle, breaking it across lines when
// it will not fit on one, and returns the y below the last line.
//
// Each line is measured against the circle's width at its own height, and
// centred. A word longer than the line is left to overflow the wrap rather
// than being cut, because a truncated model number is worse than a wide one —
// and the containment test would catch it if it ever left the disc.
func drawWrapped(c *render.Canvas, fonts render.FontFamily, y int, s string, ink epaper.Ink) int {
	f, err := fonts(13)
	if err != nil {
		return y
	}
	lh := render.LineHeight(f)

	for _, line := range wrapToCircle(s, f, y, lh) {
		// Stop at the bottom of the disc rather than drawing through it.
		// Silently dropping a line is the lesser evil: the alternative is
		// text spilling onto the dithered field, where it is unreadable and
		// looks like a rendering fault.
		if y+lh > circleBottom() {
			break
		}
		w := render.MeasureText(line, f)
		c.Text(image.Pt(circleX-w/2, y), line, f, ink)
		y += lh
	}
	return y
}

// circleBottom is the lowest y at which text may still be drawn inside the
// disc, with a margin so a descender cannot cross the outline.
func circleBottom() int { return circleY + circleR - 6 }

// wrapToCircle breaks s into lines that fit the circle at successive heights.
func wrapToCircle(s string, f font.Face, y, lineHeight int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	cur := ""
	for _, word := range words {
		width := 2 * inscribedHalfWidth(maxAbs(y-circleY, y+lineHeight-circleY))
		candidate := word
		if cur != "" {
			candidate = cur + " " + word
		}
		if render.MeasureText(candidate, f) <= width || cur == "" {
			cur = candidate
			continue
		}
		lines = append(lines, cur)
		y += lineHeight
		cur = word
	}
	return append(lines, cur)
}

// fitLine draws one centred line inside the circle and returns the y below it.
// The box is as wide as the circle permits across the line's full height, not
// at its midpoint, so a tall line cannot poke out at its top or bottom corner.
func fitLine(c *render.Canvas, fonts render.FontFamily, y, height int, s string, ink epaper.Ink) int {
	half := inscribedHalfWidth(maxAbs(y-circleY, y+height-circleY))
	c.TextFitted(image.Rect(circleX-half, y, circleX+half, y+height), s, fonts, ink)
	return y + height
}

// inscribedHalfWidth is the half-width of the circle at a vertical offset dy
// from its centre, less a margin.
//
// The margin is not decoration. TextFitted grows text until it fills the box,
// so whatever this returns is exactly how wide the text becomes; with a small
// margin every line ends up touching the circle's outline and reads as though
// it has burst out of it.
func inscribedHalfWidth(dy int) int {
	const margin = 10
	d := circleR*circleR - dy*dy
	if d <= 0 {
		return 0
	}
	return isqrt(d) - margin
}

// isqrt is an integer square root, so the layout has no floating point in it
// and is identical everywhere.
func isqrt(n int) int {
	x := 0
	for (x+1)*(x+1) <= n {
		x++
	}
	return x
}

func maxAbs(a, b int) int {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	return max(a, b)
}

func alternate(i int) epaper.Ink {
	if i%2 == 0 {
		return epaper.Black
	}
	return epaper.White
}

func mod(a, b int) int { return ((a % b) + b) % b }

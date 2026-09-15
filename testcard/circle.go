package testcard

// The central disc and everything inside it.
//
// This is the card's one free-form region, so it is also the only part where
// a layout can go wrong in an interesting way. Everything here is positioned
// from the circle's own geometry rather than from guessed constants, and a
// test asserts that nothing drawn for the circle escapes it.

import (
	"image"
	"strings"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/render"
	"golang.org/x/image/font"
)

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

		// A legibility ladder, smallest last.
		//
		// These sizes are chosen to land on DIFFERENT faces from the default
		// family — 17px and 13px — so the card shows two distinct renderings
		// rather than the same one twice. A family that maps them together
		// will simply draw one size twice, which is harmless.
		for _, size := range []int{17, 13} {
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

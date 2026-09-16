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
func drawCircle(c *render.Canvas, l layout, fonts render.FontFamily, title string, lines []string) {
	disc := image.Rect(l.circleX-l.circleR, l.circleY-l.circleR, l.circleX+l.circleR+1, l.circleY+l.circleR+1)
	c.Ellipse(disc, epaper.White)
	c.StrokeEllipse(disc, epaper.Black)

	// Everything inside is laid out top to bottom from a running cursor, so
	// adding or resizing an element cannot silently land on top of the next
	// one. A first attempt used fixed offsets and the legibility ladder drew
	// straight over the pixel grid.
	// The cursor starts a little below the top of the disc, proportionally,
	// so text does not begin hard against the outline on any panel.
	y := l.circleY - l.circleR + atLeast(l.circleR*16/78, 3)

	if fonts != nil {
		// The heading, if the disc is big enough to hold one.
		//
		// On a small panel it is not: the bundled bitmap faces have a minimum
		// line height of about 13px, and near the top of a 62px disc the
		// inscribed width is only about 30px. A card without a heading is the
		// right answer there — the heading is the one element on the card that
		// measures nothing. Asking first, rather than letting TextFitted fail,
		// is what keeps the rest of the card drawable.
		y = fitLine(c, l, fonts, y, atLeast(l.circleR*20/78, 13), title, epaper.Red)

		// Caller lines. A panel model off an EEPROM can be longer than the
		// circle is wide — "Red/Yellow wHAT (JD79668)" in a monospace bitmap
		// face needs 175px and the circle is 156 at its widest — so these
		// wrap rather than shrink.
		for _, line := range lines {
			if line == "" {
				continue
			}
			y = drawWrapped(c, l, fonts, y+1, line, epaper.Black)
			if y >= l.circleBottom() {
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
			if y+lh > l.circleBottom() {
				break // out of room; the caller gave us a lot of lines
			}
			half := l.inscribedHalfWidth(maxAbs(y-l.circleY, y+lh-l.circleY))
			text := "Hamburgefonstiv"
			if render.MeasureText(text, f) > 2*half {
				text = "Hamburgef"
			}
			c.Text(image.Pt(l.circleX-render.MeasureText(text, f)/2, y), text, f, epaper.Black)
			y += lh + 1
		}
		y += 3
	}

	// A 1px grid: if any of it greys out, the panel is not resolving pixels.
	gridH := atLeast(l.circleR*22/78, 6)
	if half := l.inscribedHalfWidth(maxAbs(y-l.circleY, y+gridH-l.circleY)); half > 12 {
		c.Checker(image.Rect(l.circleX-half, y, l.circleX+half, y+gridH), epaper.Black, epaper.White, 1)
		c.StrokeRect(image.Rect(l.circleX-half, y, l.circleX+half, y+gridH), epaper.Black)
		y += gridH + 2
	}

	// Flat accent patches, for judging the inks against the dithered mixes
	// in the ladders either side of the card.
	patchH := atLeast(l.circleR*14/78, 5)
	if half := l.inscribedHalfWidth(maxAbs(y-l.circleY, y+patchH-l.circleY)); half > 12 {
		c.Checker(image.Rect(l.circleX-half, y, l.circleX, y+patchH), epaper.Yellow, epaper.Red, 1)
		c.Rect(image.Rect(l.circleX+1, y, l.circleX+half, y+patchH), epaper.Red)
		c.StrokeRect(image.Rect(l.circleX-half, y, l.circleX+half, y+patchH), epaper.Black)
	}
}

// drawWrapped draws a string inside the circle, breaking it across lines when
// it will not fit on one, and returns the y below the last line.
//
// Each line is measured against the circle's width at its own height, and
// centred. A word longer than the line is left to overflow the wrap rather
// than being cut, because a truncated model number is worse than a wide one —
// and the containment test would catch it if it ever left the disc.
func drawWrapped(c *render.Canvas, l layout, fonts render.FontFamily, y int, s string, ink epaper.Ink) int {
	f, err := fonts(13)
	if err != nil {
		return y
	}
	lh := render.LineHeight(f)

	for _, line := range wrapToCircle(l, s, f, y, lh) {
		// Stop at the bottom of the disc rather than drawing through it.
		// Silently dropping a line is the lesser evil: the alternative is
		// text spilling onto the dithered field, where it is unreadable and
		// looks like a rendering fault.
		if y+lh > l.circleBottom() {
			break
		}
		w := render.MeasureText(line, f)
		c.Text(image.Pt(l.circleX-w/2, y), line, f, ink)
		y += lh
	}
	return y
}

// wrapToCircle breaks s into lines that fit the circle at successive heights.
//
// Words are kept whole where they can be. A word too wide for the disc even on
// its own is broken across lines rather than left to overflow: on a 250x122
// panel the disc is about 56px at its widest, which is eight characters of the
// 13px bitmap face, and "Red/Yellow" alone is wider than that. Letting it
// overflow put the model name out through the side of the circle and onto the
// dithered field, where it reads as a rendering fault rather than as a long
// name — caught by looking at the 250x122 golden.
//
// Breaking mid-word is ugly. It is still the best of the three options: the
// alternatives are dropping the line, which loses the identity of the run, and
// shrinking the text, which cannot work — 13px is the smallest face there is
// here, and the bench found 10px to be the legibility floor on this hardware.
func wrapToCircle(l layout, s string, f font.Face, y, lineHeight int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}

	widthAt := func(y int) int {
		return 2 * l.inscribedHalfWidth(maxAbs(y-l.circleY, y+lineHeight-l.circleY))
	}

	var lines []string
	cur := ""
	flush := func() {
		lines = append(lines, cur)
		y += lineHeight
		cur = ""
	}

	for _, word := range words {
		candidate := word
		if cur != "" {
			candidate = cur + " " + word
		}
		if render.MeasureText(candidate, f) <= widthAt(y) {
			cur = candidate
			continue
		}
		if cur != "" {
			flush()
		}
		// The word on its own. If it still does not fit, break it by
		// characters, greedily, re-measuring at each line's own height.
		for render.MeasureText(word, f) > widthAt(y) {
			cut := longestPrefixThatFits(word, f, widthAt(y))
			if cut == 0 {
				// The disc is too narrow for even one character at this
				// height. Nothing sensible can be drawn; give up rather than
				// loop forever.
				return lines
			}
			lines = append(lines, word[:cut])
			y += lineHeight
			word = word[cut:]
		}
		cur = word
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// longestPrefixThatFits returns the length in bytes of the longest prefix of s
// that measures no wider than width, never splitting a multi-byte rune.
func longestPrefixThatFits(s string, f font.Face, width int) int {
	best := 0
	for i := range s { // range over a string yields rune boundaries
		if i == 0 {
			continue
		}
		if render.MeasureText(s[:i], f) > width {
			return best
		}
		best = i
	}
	if render.MeasureText(s, f) <= width {
		return len(s)
	}
	return best
}

// fitLine draws one centred line inside the circle and returns the y below it.
// The box is as wide as the circle permits across the line's full height, not
// at its midpoint, so a tall line cannot poke out at its top or bottom corner.
//
// If nothing fits, nothing is drawn and the cursor does not move: the caller
// gets the space back for whatever comes next, and the canvas is left usable.
// See the note at the call site on why the card degrades rather than fails.
func fitLine(c *render.Canvas, l layout, fonts render.FontFamily, y, height int, s string, ink epaper.Ink) int {
	half := l.inscribedHalfWidth(maxAbs(y-l.circleY, y+height-l.circleY))
	box := image.Rect(l.circleX-half, y, l.circleX+half, y+height)
	if _, _, err := render.FittedSize(box, s, fonts); err != nil {
		return y
	}
	c.TextFitted(box, s, fonts, ink)
	return y + height
}

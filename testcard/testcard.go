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

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/render"
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

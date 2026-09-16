package testcard

// This file holds the orientation card; the package doc is in testcard.go.

import (
	"image"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/render"
)

// DrawOrientation paints a card that says, at a glance, which way up the panel
// is drawing.
//
// It exists because a driver can be wrong in a way nothing else catches. The
// JD79661 sends its frame rotated a quarter turn and padded on one axis, so a
// sign error there produces a picture that is complete, correctly coloured,
// and upside down or mirrored — which every byte-level test in the repo would
// pass, and which a photograph settles in one look.
//
// Unlike [Draw] and [DrawConformance], this scales to any panel: it is drawn
// from the canvas's own bounds, so it is the right thing to put on a panel
// whose geometry nothing else in this package knows about.
//
// # How to read it
//
// The four corners carry different inks, and the shapes in them are different
// too, so all eight ways of getting it wrong — four rotations, each with or
// without a mirror — produce a visibly different card:
//
//	top-left      solid RED square
//	top-right     solid YELLOW square, half the size
//	bottom-left   BLACK square with a white hole
//	bottom-right  black/white 1px checker
//
// A rule runs along the top edge only, and a second down the left edge only,
// so an edge that has lost pixels is obvious and so is a flip. The arrow in
// the middle points at the top-left corner.
func DrawOrientation(c *render.Canvas) {
	b := c.Bounds()
	w, h := b.Dx(), b.Dy()

	// Marker size scales with the panel, within reason: big enough to see on
	// a 2.13", small enough not to swallow a 4.2".
	m := min(w, h) / 5
	m = max(m, 8)

	c.Fill(epaper.White)

	// Edge rules: top and left only. An asymmetric frame is what makes a
	// 180-degree rotation obvious at arm's length.
	const rule = 3
	c.Rect(image.Rect(0, 0, w, rule), epaper.Black)
	c.Rect(image.Rect(0, 0, rule, h), epaper.Red)

	// Top-left: solid red.
	c.Rect(image.Rect(rule, rule, rule+m, rule+m), epaper.Red)

	// Top-right: solid yellow, half the size, so a mirror about the vertical
	// axis cannot be mistaken for the real thing.
	half := max(m/2, 4)
	c.Rect(image.Rect(w-half, rule, w, rule+half), epaper.Yellow)

	// Bottom-left: black square with a white hole punched in it.
	c.Rect(image.Rect(rule, h-m, rule+m, h), epaper.Black)
	c.Rect(image.Rect(rule+m/4, h-m+m/4, rule+m-m/4, h-m/4), epaper.White)

	// Bottom-right: a 1px checker, which also proves the panel resolves
	// single pixels at the far corner from the origin.
	c.Checker(image.Rect(w-m, h-m, w, h), epaper.Black, epaper.White, 1)

	// Single pixels hard against each corner, in the four inks. These catch
	// an off-by-one at the very edge — the one place clipping bugs live —
	// and they are the last thing to survive a geometry error.
	c.Rect(image.Rect(w-1, 0, w, 1), epaper.Red)
	c.Rect(image.Rect(0, h-1, 1, h), epaper.Yellow)

	// An arrow pointing at the top-left corner, drawn from the centre. Two
	// strokes and a head, all in integer arithmetic.
	cx, cy := w/2, h/2
	arm := min(w, h) / 4
	for i := range arm {
		c.Rect(image.Rect(cx-i, cy-i, cx-i+1, cy-i+1), epaper.Black)
		c.Rect(image.Rect(cx-i, cy-i+1, cx-i+1, cy-i+2), epaper.Black)
	}
	head := max(arm/3, 3)
	c.Rect(image.Rect(cx-arm, cy-arm, cx-arm+head, cy-arm+1), epaper.Black)
	c.Rect(image.Rect(cx-arm, cy-arm, cx-arm+1, cy-arm+head), epaper.Black)
}

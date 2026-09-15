// Package consumertest shows how to test your own display code.
//
// The trick is not a trick: keep the drawing a pure function from data to a
// canvas, and it becomes testable like any other function. No Pi, no panel, no
// 25-second wait, and it runs in CI.
//
//	func Draw(c *render.Canvas, s State) error
//
// That signature is the whole design. The program that fetches data and drives
// the panel calls it; so does the test. Nothing about the hardware leaks in.
//
// See display_test.go, which is the actual point of this package.
package consumertest

import (
	"fmt"
	"image"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/render"
	"github.com/sweeney/epaper/testcard"
)

// State is everything the display shows.
type State struct {
	Name string
	// Status is shown large. Anything other than "OK" is drawn in the accent
	// ink, because on a panel nobody watches, colour has to mean something.
	Status string
	Detail string
}

// Draw paints the state onto a canvas.
//
// It returns an error rather than silently clipping, which matters more here
// than it would on a screen: e-ink has no scrollbar and no overflow
// indicator, so text that does not fit simply is not there, and nobody is
// looking at the moment it happens.
func Draw(c *render.Canvas, s State) error {
	fonts := testcard.Fonts()
	b := c.Bounds()
	w := b.Dx()

	c.Fill(epaper.White)

	// Name across the top.
	c.TextFitted(image.Rect(10, 10, w-10, 44), s.Name, fonts, epaper.Black)

	// Status, large, in an accent ink when it is not OK.
	ink := epaper.Black
	if s.Status != "OK" {
		ink = epaper.Red
	}
	c.TextFitted(image.Rect(10, 56, w-10, 120), s.Status, fonts, ink)

	// Detail wraps, and reports if it could not all be shown.
	detail, err := fonts(13)
	if err != nil {
		return err
	}
	c.TextWrapped(image.Rect(10, 132, w-10, b.Max.Y-10), s.Detail, detail, epaper.Black)

	if err := c.Err(); err != nil {
		return fmt.Errorf("consumertest: %w", err)
	}
	return nil
}

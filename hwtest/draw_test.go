//go:build hardware

package hwtest

import (
	"context"
	"testing"
	"time"

	"github.com/sweeney/epaper/inky"
	"github.com/sweeney/epaper/render"
	"github.com/sweeney/epaper/testcard"
)

// drawTimeout is generous: a refresh is about 20 s measured, and a stuck panel
// should fail the test rather than hang the run.
const drawTimeout = 2 * time.Minute

// TestDrawConformance puts the conformance pattern on the glass.
//
// These exact pixels are already proven byte-identical to the vendor's output
// by the render tests, so anything wrong on the panel is in the transport, the
// command sequence or the wiring — not in the drawing. That is what makes this
// worth 20 seconds.
//
// The four single-pixel corner markers are the thing to look at: red at
// (396,296), white at (399,296), yellow at (396,299), black at (399,299). A
// flip or a transposition leaves the rest of the pattern looking plausible and
// moves those.
func TestDrawConformance(t *testing.T) {
	dev, err := inky.Open()
	if err != nil {
		t.Fatalf("inky.Open(): %v", err)
	}
	defer dev.Close()

	c := render.NewCanvasFor(dev)
	testcard.DrawConformance(c)
	if err := c.Err(); err != nil {
		t.Fatalf("drawing: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), drawTimeout)
	defer cancel()

	started := time.Now()
	if err := dev.Show(ctx, c.Image()); err != nil {
		t.Fatalf("Show(): %v", err)
	}
	t.Logf("conformance refresh took %s", time.Since(started).Round(time.Millisecond))
}

// TestDrawOrientation is the acceptance test for a driver that rotates.
//
// The JD79661 sends its frame turned a quarter turn and padded on one axis. A
// sign error there yields a picture that is complete, correctly coloured and
// upside down — which every byte-level test in this repo passes. Nothing but
// looking at the glass settles it, so this draws a card built for looking at:
// four differently-inked corners and an arrow pointing at the top-left one.
// See testcard.DrawOrientation for how to read it.
//
// It runs before the test card so that on a panel the test card cannot fit,
// this is what is left showing.
func TestDrawOrientation(t *testing.T) {
	dev, err := inky.Open()
	if err != nil {
		t.Fatalf("inky.Open(): %v", err)
	}
	defer dev.Close()

	c := render.NewCanvasFor(dev)
	testcard.DrawOrientation(c)
	if err := c.Err(); err != nil {
		t.Fatalf("drawing: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), drawTimeout)
	defer cancel()

	started := time.Now()
	if err := dev.Show(ctx, c.Image()); err != nil {
		t.Fatalf("Show(): %v", err)
	}
	t.Logf("orientation refresh took %s", time.Since(started).Round(time.Millisecond))
	t.Log("LOOK AT THE PANEL: red square top-left, small yellow square top-right, " +
		"black square with a white hole bottom-left, checker bottom-right, " +
		"arrow pointing top-left. A black rule along the top edge only, a red one down the left.")
}

// TestDrawTestCard is the visual acceptance test, and it runs last so it is
// what the panel is left showing.
//
// The card lays itself out from the panel's own bounds, so this runs on every
// supported panel — a 250x122 pHAT gets the same elements as a 400x300 wHAT in
// a smaller frame. It did not always: the layout was absolute pixels, and on
// the pHAT the disc fell off the bottom edge. See PLAN §13.4.
func TestDrawTestCard(t *testing.T) {
	dev, err := inky.Open()
	if err != nil {
		t.Fatalf("inky.Open(): %v", err)
	}
	defer dev.Close()

	ctx, cancel := context.WithTimeout(context.Background(), drawTimeout)
	defer cancel()

	// testcard.Show is the whole thing in one call, which is also what a
	// consumer would write — so this exercises the documented path.
	started := time.Now()
	if err := testcard.Show(ctx, dev, time.Now().Format("2006-01-02 15:04")); err != nil {
		t.Fatalf("testcard.Show(): %v", err)
	}
	t.Logf("test card refresh took %s", time.Since(started).Round(time.Millisecond))
}

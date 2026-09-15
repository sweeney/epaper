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

// TestDrawTestCard is the visual acceptance test, and it runs last so it is
// what the panel is left showing.
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

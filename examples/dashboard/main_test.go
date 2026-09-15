package main

import (
	"image"
	"strings"
	"testing"

	"github.com/sweeney/epaper/render"
)

// Example code is still code. These are also the tests a reader should copy:
// Draw takes only a canvas and data, so it can be checked exactly like any
// other function.

func canvas() *render.Canvas {
	return render.NewCanvas(image.Rect(0, 0, 400, 300), panelPalette)
}

func TestDrawSample(t *testing.T) {
	c := canvas()
	if err := Draw(c, sample()); err != nil {
		t.Fatalf("Draw(): %v", err)
	}
	if len(inksUsed(c)) < 3 {
		t.Errorf("the dashboard used %d inks; it should use black, white, red and yellow", len(inksUsed(c)))
	}
}

// An alert is the only thing that should put accent ink on the panel. Colour
// that appears for decoration trains the eye to ignore it.
func TestAlertsUseTheAccentInk(t *testing.T) {
	quiet := sample()
	for i := range quiet.Readings {
		quiet.Readings[i].Alert = false
	}

	calm, loud := canvas(), canvas()
	if err := Draw(calm, quiet); err != nil {
		t.Fatal(err)
	}
	if err := Draw(loud, sample()); err != nil {
		t.Fatal(err)
	}

	// The header is red in both, so compare the body rather than the whole.
	if bodyRed(calm) != 0 {
		t.Error("a dashboard with no alerts still has red in its body")
	}
	if bodyRed(loud) == 0 {
		t.Error("a dashboard with an alert has no red in its body")
	}
}

// Running out of room must be reported. Silently dropping the last reading is
// exactly the failure e-ink makes invisible.
func TestTooManyReadingsIsReported(t *testing.T) {
	s := sample()
	for i := range 20 {
		s.Readings = append(s.Readings, Reading{Label: "extra", Value: string(rune('a' + i))})
	}

	err := Draw(canvas(), s)
	if err == nil {
		t.Fatal("Draw() = nil with far too many readings")
	}
	if !strings.Contains(err.Error(), "do not fit") {
		t.Errorf("error %q does not say what went wrong", err)
	}
}

// The gauge must cope with values outside 0..1 rather than drawing outside its
// own box.
func TestGaugeClamps(t *testing.T) {
	for _, ratio := range []float64{-5, 0, 0.5, 1, 7} {
		s := sample()
		s.Gauge.Ratio = ratio
		if err := Draw(canvas(), s); err != nil {
			t.Errorf("Draw() with gauge ratio %v: %v", ratio, err)
		}
	}
	if got := clamp(-1); got != 0 {
		t.Errorf("clamp(-1) = %v, want 0", got)
	}
	if got := clamp(2); got != 1 {
		t.Errorf("clamp(2) = %v, want 1", got)
	}
}

func TestRenderTo(t *testing.T) {
	img, err := renderTo(400, 300, panelPalette, sample())
	if err != nil {
		t.Fatalf("renderTo(): %v", err)
	}
	if got := img.Bounds(); got != image.Rect(0, 0, 400, 300) {
		t.Errorf("bounds = %v, want 400x300", got)
	}
}

// bodyRed counts red pixels below the header.
func bodyRed(c *render.Canvas) int {
	n := 0
	for y := 60; y < 300; y++ {
		for x := range 400 {
			if c.Image().ColorIndexAt(x, y) == 3 {
				n++
			}
		}
	}
	return n
}

func inksUsed(c *render.Canvas) []uint8 {
	seen := map[uint8]bool{}
	for _, p := range c.Image().Pix {
		seen[p] = true
	}
	var out []uint8
	for k := range seen {
		out = append(out, k)
	}
	return out
}

package consumertest

import (
	"context"
	"errors"
	"image"
	"image/color"
	"testing"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/mock"
	"github.com/sweeney/epaper/render"
)

// The panel this code is written for. Declaring it once here is all the
// "hardware" a test of display code needs.
var panel = epaper.Palette{
	{Ink: epaper.Black, RGB: color.RGBA{0, 0, 0, 255}},
	{Ink: epaper.White, RGB: color.RGBA{255, 255, 255, 255}},
	{Ink: epaper.Yellow, RGB: color.RGBA{235, 205, 40, 255}},
	{Ink: epaper.Red, RGB: color.RGBA{190, 45, 40, 255}},
}

func canvas() *render.Canvas {
	return render.NewCanvas(image.Rect(0, 0, 400, 300), panel)
}

// The most basic thing worth asserting: it draws without complaining.
func TestDrawSucceeds(t *testing.T) {
	c := canvas()
	if err := Draw(c, State{Name: "Greenhouse", Status: "OK", Detail: "All sensors reporting."}); err != nil {
		t.Fatalf("Draw(): %v", err)
	}
}

// Assert that data actually reaches the screen, without needing to read the
// text back. Two states that differ must produce different pixels; if they do
// not, something is being dropped.
func TestStatusReachesTheScreen(t *testing.T) {
	ok := canvas()
	if err := Draw(ok, State{Name: "Greenhouse", Status: "OK"}); err != nil {
		t.Fatal(err)
	}
	bad := canvas()
	if err := Draw(bad, State{Name: "Greenhouse", Status: "FROST"}); err != nil {
		t.Fatal(err)
	}

	if identical(ok.Image(), bad.Image()) {
		t.Error("two different statuses produced identical pixels; the status is not being drawn")
	}
}

// Colour has to mean something. A fault must put accent ink on the panel, and
// a healthy state must not — otherwise the eye learns to ignore it.
func TestOnlyFaultsUseTheAccentInk(t *testing.T) {
	for _, tc := range []struct {
		status    string
		wantAlert bool
	}{
		{"OK", false},
		{"FROST", true},
		{"OFFLINE", true},
	} {
		c := canvas()
		if err := Draw(c, State{Name: "Greenhouse", Status: tc.status}); err != nil {
			t.Fatal(err)
		}
		red := count(c.Image(), 3)
		if got := red > 0; got != tc.wantAlert {
			t.Errorf("status %q: red ink present = %v, want %v", tc.status, got, tc.wantAlert)
		}
	}
}

// The failure e-ink makes easy: text that does not fit is simply absent, and
// there is nobody watching to notice. Draw must say so.
func TestOverlongDetailIsReported(t *testing.T) {
	long := ""
	for range 60 {
		long += "far too much detail to fit on a small panel. "
	}

	err := Draw(canvas(), State{Name: "Greenhouse", Status: "OK", Detail: long})
	if err == nil {
		t.Fatal("Draw() = nil for detail that cannot fit; it must report, not clip silently")
	}
	if !errors.Is(err, render.ErrTextDoesNotFit) {
		t.Errorf("Draw() error = %v, want it to wrap ErrTextDoesNotFit", err)
	}
}

// An ink the panel does not have must be refused, not quietly substituted.
// This is what a mono panel would do to code written for a four-ink one.
func TestMonoPanelRefusesTheAccentInk(t *testing.T) {
	mono := epaper.Palette{
		{Ink: epaper.Black, RGB: color.RGBA{0, 0, 0, 255}},
		{Ink: epaper.White, RGB: color.RGBA{255, 255, 255, 255}},
	}
	c := render.NewCanvas(image.Rect(0, 0, 400, 300), mono)

	err := Draw(c, State{Name: "Greenhouse", Status: "FROST"})
	if !errors.Is(err, render.ErrInkUnavailable) {
		t.Errorf("Draw() on a mono panel = %v, want ErrInkUnavailable", err)
	}
}

// End to end against the mock: what a real program does, minus the panel.
func TestShowOnAMockDevice(t *testing.T) {
	dev := mock.New(400, 300, panel)
	defer dev.Close()

	c := render.NewCanvasFor(dev) // geometry and palette from the device
	if err := Draw(c, State{Name: "Greenhouse", Status: "OK", Detail: "Nominal."}); err != nil {
		t.Fatal(err)
	}
	if err := dev.Show(context.Background(), c.Image()); err != nil {
		t.Fatalf("Show(): %v", err)
	}

	if len(dev.Frames()) != 1 {
		t.Fatalf("Frames() = %d, want 1", len(dev.Frames()))
	}
	// Uncomment to eyeball what the panel would show:
	//   dev.SavePNG("/tmp/consumertest.png")
}

// Exercise the error path a real program must handle. On hardware this is the
// code least likely to be right, because it almost never runs.
func TestRefreshFailureIsHandled(t *testing.T) {
	dev := mock.New(400, 300, panel)
	defer dev.Close()

	wire := errors.New("panel unplugged")
	dev.FailNextShow(wire)

	c := render.NewCanvasFor(dev)
	if err := Draw(c, State{Name: "Greenhouse", Status: "OK"}); err != nil {
		t.Fatal(err)
	}

	err := dev.Show(context.Background(), c.Image())
	if !errors.Is(err, wire) {
		t.Fatalf("Show() = %v, want the device error", err)
	}
	if len(dev.Frames()) != 0 {
		t.Error("a failed Show recorded a frame")
	}

	// ...and the next attempt succeeds, so a retry loop is testable too.
	if err := dev.Show(context.Background(), c.Image()); err != nil {
		t.Fatalf("retry: %v", err)
	}
}

func identical(a, b *image.Paletted) bool {
	if len(a.Pix) != len(b.Pix) {
		return false
	}
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			return false
		}
	}
	return true
}

func count(img *image.Paletted, idx uint8) int {
	n := 0
	for _, p := range img.Pix {
		if p == idx {
			n++
		}
	}
	return n
}

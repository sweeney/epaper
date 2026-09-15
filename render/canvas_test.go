package render_test

import (
	"errors"
	"image"
	"image/color"
	"testing"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/mock"
	"github.com/sweeney/epaper/render"
	"golang.org/x/image/font/basicfont"
)

var fourInk = epaper.Palette{
	{Ink: epaper.Black, RGB: color.RGBA{0, 0, 0, 255}},
	{Ink: epaper.White, RGB: color.RGBA{255, 255, 255, 255}},
	{Ink: epaper.Yellow, RGB: color.RGBA{235, 205, 40, 255}},
	{Ink: epaper.Red, RGB: color.RGBA{190, 45, 40, 255}},
}

const (
	black  = 0
	white  = 1
	yellow = 2
	red    = 3
)

func newCanvas(w, h int) *render.Canvas {
	return render.NewCanvas(image.Rect(0, 0, w, h), fourInk)
}

// at reports the palette index at (x,y).
func at(t *testing.T, c *render.Canvas, x, y int) uint8 {
	t.Helper()
	return c.Image().ColorIndexAt(x, y)
}

func TestNewCanvasForTakesGeometryFromTheDevice(t *testing.T) {
	d := mock.New(400, 300, fourInk)
	defer d.Close()

	c := render.NewCanvasFor(d)
	if got := c.Image().Bounds(); got != d.Bounds() {
		t.Errorf("canvas bounds = %v, want %v", got, d.Bounds())
	}
	// The whole point: the image it produces is showable without adjustment.
	if err := d.Show(t.Context(), c.Image()); err != nil {
		t.Errorf("Show() of a NewCanvasFor image: %v", err)
	}
}

func TestFill(t *testing.T) {
	c := newCanvas(4, 2)
	c.Fill(epaper.Red)
	if err := c.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
	for y := range 2 {
		for x := range 4 {
			if got := at(t, c, x, y); got != red {
				t.Fatalf("pixel (%d,%d) = %d, want %d", x, y, got, red)
			}
		}
	}
}

func TestSet(t *testing.T) {
	c := newCanvas(4, 2)
	c.Fill(epaper.White)
	c.Set(1, 1, epaper.Black)
	if got := at(t, c, 1, 1); got != black {
		t.Errorf("pixel (1,1) = %d, want %d", got, black)
	}
	if got := at(t, c, 0, 0); got != white {
		t.Errorf("pixel (0,0) = %d, want %d — Set touched more than one pixel", got, white)
	}
}

// Drawing outside the canvas is clipped, not an error. A layout that runs off
// the edge is a bug, but it is the caller's bug to find with TextFitted and
// goldens, not something to fail a draw call over.
func TestDrawingIsClipped(t *testing.T) {
	c := newCanvas(4, 2)
	c.Fill(epaper.White)
	c.Set(-1, 0, epaper.Black)
	c.Set(0, -1, epaper.Black)
	c.Set(4, 0, epaper.Black)
	c.Set(0, 2, epaper.Black)
	c.Rect(image.Rect(-10, -10, 20, 20), epaper.Red)
	c.Line(image.Pt(-50, -50), image.Pt(50, 50), epaper.Black)
	if err := c.Err(); err != nil {
		t.Errorf("Err() = %v, want nil — clipping is not an error", err)
	}
}

func TestRectIsHalfOpen(t *testing.T) {
	c := newCanvas(6, 4)
	c.Fill(epaper.White)
	c.Rect(image.Rect(1, 1, 3, 3), epaper.Black)

	// Go's rectangles are half-open: Max is excluded.
	for _, p := range []image.Point{{1, 1}, {2, 1}, {1, 2}, {2, 2}} {
		if got := at(t, c, p.X, p.Y); got != black {
			t.Errorf("pixel (%d,%d) = %d, want black (inside the rect)", p.X, p.Y, got)
		}
	}
	for _, p := range []image.Point{{0, 1}, {3, 1}, {1, 0}, {1, 3}, {3, 3}} {
		if got := at(t, c, p.X, p.Y); got != white {
			t.Errorf("pixel (%d,%d) = %d, want white (outside the rect)", p.X, p.Y, got)
		}
	}
}

func TestStrokeRect(t *testing.T) {
	c := newCanvas(6, 6)
	c.Fill(epaper.White)
	c.StrokeRect(image.Rect(1, 1, 5, 5), epaper.Black)

	// A 1px border on the half-open rect [1,5) x [1,5).
	for _, p := range []image.Point{{1, 1}, {4, 1}, {1, 4}, {4, 4}, {2, 1}, {1, 2}} {
		if got := at(t, c, p.X, p.Y); got != black {
			t.Errorf("pixel (%d,%d) = %d, want black (on the border)", p.X, p.Y, got)
		}
	}
	if got := at(t, c, 2, 2); got != white {
		t.Errorf("pixel (2,2) = %d, want white — StrokeRect filled the interior", got)
	}
}

func TestLineEndpointsAreIncluded(t *testing.T) {
	c := newCanvas(8, 8)
	c.Fill(epaper.White)
	c.Line(image.Pt(1, 1), image.Pt(6, 6), epaper.Black)

	if got := at(t, c, 1, 1); got != black {
		t.Errorf("start point (1,1) = %d, want black", got)
	}
	if got := at(t, c, 6, 6); got != black {
		t.Errorf("end point (6,6) = %d, want black", got)
	}
	for i := 1; i <= 6; i++ {
		if got := at(t, c, i, i); got != black {
			t.Errorf("diagonal pixel (%d,%d) = %d, want black", i, i, got)
		}
	}
}

func TestLineHorizontalAndVertical(t *testing.T) {
	c := newCanvas(8, 8)
	c.Fill(epaper.White)
	c.Line(image.Pt(1, 3), image.Pt(6, 3), epaper.Black)
	c.Line(image.Pt(3, 1), image.Pt(3, 6), epaper.Red)

	for x := 1; x <= 6; x++ {
		if got := at(t, c, x, 3); got == white {
			t.Errorf("pixel (%d,3) is unset; the horizontal line is broken", x)
		}
	}
	for y := 1; y <= 6; y++ {
		if got := at(t, c, 3, y); got == white {
			t.Errorf("pixel (3,%d) is unset; the vertical line is broken", y)
		}
	}
}

// A single-point line must still plot its one pixel, not loop forever.
func TestLineDegenerate(t *testing.T) {
	c := newCanvas(4, 4)
	c.Fill(epaper.White)
	c.Line(image.Pt(2, 2), image.Pt(2, 2), epaper.Black)
	if got := at(t, c, 2, 2); got != black {
		t.Errorf("pixel (2,2) = %d, want black", got)
	}
}

func TestEllipse(t *testing.T) {
	c := newCanvas(11, 11)
	c.Fill(epaper.White)
	c.Ellipse(image.Rect(0, 0, 11, 11), epaper.Black) // a circle, centre (5,5)

	// Centre and the four extremes are inside; the corners are not.
	for _, p := range []image.Point{{5, 5}, {0, 5}, {10, 5}, {5, 0}, {5, 10}} {
		if got := at(t, c, p.X, p.Y); got != black {
			t.Errorf("pixel (%d,%d) = %d, want black (inside)", p.X, p.Y, got)
		}
	}
	for _, p := range []image.Point{{0, 0}, {10, 0}, {0, 10}, {10, 10}} {
		if got := at(t, c, p.X, p.Y); got != white {
			t.Errorf("pixel (%d,%d) = %d, want white (a corner is outside)", p.X, p.Y, got)
		}
	}
}

func TestStrokeEllipseIsHollow(t *testing.T) {
	c := newCanvas(11, 11)
	c.Fill(epaper.White)
	c.StrokeEllipse(image.Rect(0, 0, 11, 11), epaper.Black)

	if got := at(t, c, 5, 5); got != white {
		t.Errorf("centre (5,5) = %d, want white — StrokeEllipse filled the interior", got)
	}
	if got := at(t, c, 0, 5); got != black {
		t.Errorf("pixel (0,5) = %d, want black — the leftmost point is on the outline", got)
	}
}

func TestPolygon(t *testing.T) {
	c := newCanvas(9, 9)
	c.Fill(epaper.White)
	// A triangle with a flat bottom.
	c.Polygon([]image.Point{{0, 8}, {4, 0}, {8, 8}}, epaper.Black)

	if got := at(t, c, 4, 7); got != black {
		t.Errorf("pixel (4,7) = %d, want black (well inside)", got)
	}
	if got := at(t, c, 4, 0); got != black {
		t.Errorf("apex (4,0) = %d, want black — vertices are included", got)
	}
	if got := at(t, c, 0, 0); got != white {
		t.Errorf("pixel (0,0) = %d, want white (outside)", got)
	}
}

// Winding order must not matter: the same points in reverse fill the same
// pixels. A rasteriser that only handles one winding fails silently on the
// other, producing an empty shape.
func TestPolygonIsWindingAgnostic(t *testing.T) {
	pts := []image.Point{{0, 8}, {4, 0}, {8, 8}}
	rev := []image.Point{{8, 8}, {4, 0}, {0, 8}}

	a, b := newCanvas(9, 9), newCanvas(9, 9)
	a.Fill(epaper.White)
	b.Fill(epaper.White)
	a.Polygon(pts, epaper.Black)
	b.Polygon(rev, epaper.Black)

	for y := range 9 {
		for x := range 9 {
			if at(t, a, x, y) != at(t, b, x, y) {
				t.Fatalf("pixel (%d,%d) differs between the two windings", x, y)
			}
		}
	}
}

func TestPolygonTooFewPoints(t *testing.T) {
	c := newCanvas(9, 9)
	c.Fill(epaper.White)
	c.Polygon([]image.Point{{0, 0}, {4, 4}}, epaper.Black)
	if err := c.Err(); err == nil {
		t.Error("Polygon() with two points: Err() = nil, want an error")
	}
}

// PLAN §4.3: the first unavailable ink is recorded, subsequent draws are
// no-ops, and one check at the end catches it. No silent substitution.
func TestUnavailableInkIsSticky(t *testing.T) {
	c := newCanvas(4, 2)
	c.Fill(epaper.White)
	c.Rect(image.Rect(0, 0, 4, 2), epaper.Green) // not on a four-ink panel
	c.Fill(epaper.Black)                         // must be a no-op

	err := c.Err()
	if err == nil {
		t.Fatal("Err() = nil after drawing with an absent ink")
	}
	if !errors.Is(err, render.ErrInkUnavailable) {
		t.Errorf("Err() = %v, want it to wrap ErrInkUnavailable", err)
	}
	if !contains(err.Error(), "green") {
		t.Errorf("Err() = %q, want it to name the ink", err)
	}
	for y := range 2 {
		for x := range 4 {
			if got := at(t, c, x, y); got != white {
				t.Fatalf("pixel (%d,%d) = %d, want white — drawing continued after a sticky error", x, y, got)
			}
		}
	}
}

// The first error is the useful one; later ones must not overwrite it.
func TestStickyErrorKeepsTheFirst(t *testing.T) {
	c := newCanvas(4, 2)
	c.Rect(image.Rect(0, 0, 1, 1), epaper.Green)
	c.Rect(image.Rect(0, 0, 1, 1), epaper.Blue)
	if err := c.Err(); !contains(err.Error(), "green") {
		t.Errorf("Err() = %q, want the first failure (green)", err)
	}
}

func TestNewCanvasRejectsABadPalette(t *testing.T) {
	c := render.NewCanvas(image.Rect(0, 0, 4, 2), epaper.Palette{})
	if err := c.Err(); !errors.Is(err, epaper.ErrBadPalette) {
		t.Errorf("Err() = %v, want %v", err, epaper.ErrBadPalette)
	}
	c.Fill(epaper.Black) // must not panic
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// A rectangle too small to inscribe an ellipse in must do nothing, not divide
// by zero or loop.
func TestEllipseDegenerate(t *testing.T) {
	for _, r := range []image.Rectangle{
		image.Rect(0, 0, 0, 0),
		image.Rect(2, 2, 2, 5),
		image.Rect(2, 2, 5, 2),
	} {
		c := newCanvas(8, 8)
		c.Fill(epaper.White)
		c.Ellipse(r, epaper.Black)
		c.StrokeEllipse(r, epaper.Black)
		if err := c.Err(); err != nil {
			t.Errorf("Ellipse(%v): Err() = %v, want nil", r, err)
		}
	}
}

// A 1px-tall or 1px-wide box has a degenerate axis. The ellipse collapses to
// that line, which should still be drawn rather than vanishing.
func TestEllipseSinglePixelAxis(t *testing.T) {
	c := newCanvas(8, 8)
	c.Fill(epaper.White)
	c.Ellipse(image.Rect(1, 3, 6, 4), epaper.Black) // 5x1
	for x := 1; x < 6; x++ {
		if got := at(t, c, x, 3); got != black {
			t.Errorf("pixel (%d,3) = %d, want black", x, got)
		}
	}
}

func TestStrokeRectDegenerate(t *testing.T) {
	c := newCanvas(8, 8)
	c.Fill(epaper.White)
	c.StrokeRect(image.Rect(0, 0, 0, 0), epaper.Black)
	if err := c.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
}

// Polygon points outside the canvas must be clipped without the bounding box
// calculation running away.
func TestPolygonClipped(t *testing.T) {
	c := newCanvas(8, 8)
	c.Fill(epaper.White)
	c.Polygon([]image.Point{{-100, -100}, {100, -100}, {0, 100}}, epaper.Black)
	if err := c.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
	if got := at(t, c, 4, 4); got != black {
		t.Errorf("pixel (4,4) = %d, want black — the clipped polygon should still cover the canvas", got)
	}
}

// A canvas need not start at the origin. image.Paletted supports an offset
// rectangle and NewCanvasFor passes the device's bounds through unchanged, so
// every primitive has to respect Min rather than assuming (0,0).
//
// Getting this wrong shifts everything by the offset, which on a panel looks
// like a layout mistake rather than an indexing one.
func TestCanvasWithOffsetBounds(t *testing.T) {
	const ox, oy = 37, 11
	c := render.NewCanvas(image.Rect(ox, oy, ox+40, oy+30), fourInk)
	c.Fill(epaper.White)

	// Every primitive, placed relative to the offset origin.
	c.Set(ox+1, oy+1, epaper.Black)
	c.Rect(image.Rect(ox+4, oy+4, ox+10, oy+10), epaper.Red)
	c.StrokeRect(image.Rect(ox+12, oy+4, ox+20, oy+12), epaper.Black)
	c.Line(image.Pt(ox, oy+20), image.Pt(ox+39, oy+20), epaper.Black)
	c.Ellipse(image.Rect(ox+22, oy+4, ox+34, oy+16), epaper.Yellow)
	c.Polygon([]image.Point{{ox + 2, oy + 28}, {ox + 10, oy + 22}, {ox + 18, oy + 28}}, epaper.Red)
	c.Checker(image.Rect(ox+22, oy+20, ox+38, oy+28), epaper.Yellow, epaper.Red, 2)
	c.Dither(image.Rect(ox+2, oy+14, ox+18, oy+18), epaper.Black, epaper.White, 0.5)

	if err := c.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}

	for _, tc := range []struct {
		x, y int
		want uint8
		what string
	}{
		{ox + 1, oy + 1, black, "Set"},
		{ox + 5, oy + 5, red, "Rect"},
		{ox + 12, oy + 4, black, "StrokeRect edge"},
		{ox + 16, oy + 8, white, "StrokeRect interior"},
		{ox + 20, oy + 20, black, "Line"},
		{ox + 28, oy + 10, yellow, "Ellipse"},
		{ox + 10, oy + 27, red, "Polygon"},
	} {
		if got := c.Image().ColorIndexAt(tc.x, tc.y); got != tc.want {
			t.Errorf("%s: pixel (%d,%d) = %d, want %d", tc.what, tc.x, tc.y, got, tc.want)
		}
	}

	// Nothing may have been written outside the canvas, and the image must
	// still pack cleanly.
	if _, err := epaper.Pack(c.Image()); err != nil {
		t.Errorf("Pack(): %v", err)
	}
}

// Text on an offset canvas must land where it was asked for, not at the
// offset's worth of distance away from it.
func TestTextWithOffsetBounds(t *testing.T) {
	const ox, oy = 50, 20
	c := render.NewCanvas(image.Rect(ox, oy, ox+200, oy+40), fourInk)
	c.Fill(epaper.White)
	c.Text(image.Pt(ox+2, oy+2), "Hg", basicfont.Face7x13, epaper.Black)

	if err := c.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}

	found := false
	for y := oy; y < oy+20; y++ {
		for x := ox; x < ox+40; x++ {
			if c.Image().ColorIndexAt(x, y) == black {
				found = true
			}
		}
	}
	if !found {
		t.Error("no ink near the requested position; text ignored the canvas origin")
	}
}

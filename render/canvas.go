// Package render draws into the images an e-ink panel can show.
//
// Everything here is pure: no hardware, no I/O, and no dependency on a driver.
// A layout can therefore be developed and tested entirely on a laptop, which
// matters more than it sounds — a refresh takes about 20 seconds, so finding a
// clipped header on the panel costs a thousand times what finding it in a
// golden test does.
//
// # Drawing against a palette
//
// A [Canvas] is built against a panel's [epaper.Palette], which is what lets it
// resolve inks by name and report one the panel does not have:
//
//	c := render.NewCanvasFor(dev)
//	c.Fill(epaper.White)
//	c.Rect(image.Rect(0, 0, 400, 30), epaper.Red)
//	if err := c.Err(); err != nil {
//		log.Fatal(err)
//	}
//
// Draw calls return nothing. The first failure is recorded and every
// subsequent call becomes a no-op, so one check at the end catches it — the
// same arrangement [bufio.Writer] uses, and for the same reason: drawing code
// that checks an error on every line is miserable to write and therefore does
// not get written.
//
// An unavailable ink is never silently substituted. Quietly swapping black for
// green would produce a panel that looks plausible and is wrong, which is the
// worst outcome on a display nobody is watching. A caller who wants a fallback
// asks for one with [epaper.Palette.NearestTo].
//
// # Geometry conventions
//
// Rectangles are Go's: half-open, so [image.Rect](0, 0, 4, 2) covers x in
// {0,1,2,3} and y in {0,1}. Points on a [Canvas.Line] or a [Canvas.Polygon]
// edge are drawn, so both endpoints of a line appear. Drawing outside the
// canvas is clipped rather than reported — a layout that runs off the edge is
// a bug, but it is one for [Canvas.TextFitted] and golden tests to catch, not
// something to fail a draw call over.
//
// The rasterisation rules are specified in testdata/README.md, not inherited
// from another library, and the conformance fixture pins them.
package render

import (
	"errors"
	"fmt"
	"image"

	"github.com/sweeney/epaper"
)

// Errors this package records on a [Canvas]. Match with [errors.Is] on the
// result of [Canvas.Err].
var (
	// ErrInkUnavailable means a draw call named an ink the panel does not
	// have. Nothing is substituted: a panel that looks plausible and is
	// wrong is the worst outcome on a display nobody is watching.
	ErrInkUnavailable = errors.New("render: ink not available on this palette")

	// ErrTextDoesNotFit means a string could not be drawn in the space
	// given — at any size for [Canvas.TextFitted], or in full for
	// [Canvas.TextWrapped].
	ErrTextDoesNotFit = errors.New("render: text does not fit")
)

// Canvas is a drawing surface bound to a panel's palette.
//
// The zero value is not usable; call [NewCanvas] or [NewCanvasFor]. A Canvas is
// not safe for concurrent use.
type Canvas struct {
	img     *image.Paletted
	palette epaper.Palette
	err     error
}

// NewCanvas returns a canvas of the given bounds, drawing in the given palette.
//
// An invalid palette is reported through [Canvas.Err] rather than returned, so
// that construction and drawing share one error path. The canvas is still safe
// to use; every draw call is a no-op.
func NewCanvas(r image.Rectangle, p epaper.Palette) *Canvas {
	c := &Canvas{
		img:     image.NewPaletted(r, p.Colors()),
		palette: p,
	}
	if err := p.Validate(); err != nil {
		c.err = fmt.Errorf("render: %w", err)
	}
	return c
}

// NewCanvasFor returns a canvas matching a device's geometry and palette.
//
// This is the call almost everyone writes: taking both from the device means
// the two cannot disagree, and the resulting image is showable without
// adjustment.
func NewCanvasFor(d epaper.Device) *Canvas {
	return NewCanvas(d.Bounds(), d.Palette())
}

// Image returns the canvas's image, ready to pass to [epaper.Device.Show].
// It is the live image, not a copy: drawing continues to affect it.
func (c *Canvas) Image() *image.Paletted { return c.img }

// Err returns the first error any draw call encountered, or nil.
//
// Check it once, after drawing. Once it is non-nil every subsequent draw call
// is a no-op, so an early failure cannot be papered over by later output.
func (c *Canvas) Err() error { return c.err }

// Bounds returns the canvas's geometry.
func (c *Canvas) Bounds() image.Rectangle { return c.img.Bounds() }

// ink resolves an ink to its wire index, recording a sticky error if the
// palette does not have it. It reports false if drawing should not proceed,
// which includes the case where an earlier call already failed.
func (c *Canvas) ink(i epaper.Ink) (uint8, bool) {
	if c.err != nil {
		return 0, false
	}
	idx, ok := c.palette.Index(i)
	if !ok {
		c.err = fmt.Errorf("render: ink %s is not on this panel: %w", i, ErrInkUnavailable)
		return 0, false
	}
	return idx, true
}

// fail records a sticky error, keeping the first one. The first failure is the
// one that explains the others.
func (c *Canvas) fail(err error) {
	if c.err == nil {
		c.err = err
	}
}

// setIndex writes a resolved index, clipped to the canvas.
func (c *Canvas) setIndex(x, y int, idx uint8) {
	if !(image.Pt(x, y).In(c.img.Rect)) {
		return
	}
	c.img.Pix[c.img.PixOffset(x, y)] = idx
}

// Fill paints the whole canvas.
func (c *Canvas) Fill(i epaper.Ink) {
	c.Rect(c.img.Rect, i)
}

// Set paints a single pixel. Coordinates outside the canvas are ignored.
func (c *Canvas) Set(x, y int, i epaper.Ink) {
	idx, ok := c.ink(i)
	if !ok {
		return
	}
	c.setIndex(x, y, idx)
}

// Rect fills a rectangle. The rectangle is half-open, per Go's convention: its
// Max row and column are not drawn.
func (c *Canvas) Rect(r image.Rectangle, i epaper.Ink) {
	idx, ok := c.ink(i)
	if !ok {
		return
	}
	r = r.Intersect(c.img.Rect)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c.img.Pix[c.img.PixOffset(x, y)] = idx
		}
	}
}

// StrokeRect draws a one-pixel border just inside a rectangle, leaving the
// interior untouched.
func (c *Canvas) StrokeRect(r image.Rectangle, i epaper.Ink) {
	idx, ok := c.ink(i)
	if !ok {
		return
	}
	if r.Empty() {
		return
	}
	x0, y0 := r.Min.X, r.Min.Y
	x1, y1 := r.Max.X-1, r.Max.Y-1
	for x := x0; x <= x1; x++ {
		c.setIndex(x, y0, idx)
		c.setIndex(x, y1, idx)
	}
	for y := y0; y <= y1; y++ {
		c.setIndex(x0, y, idx)
		c.setIndex(x1, y, idx)
	}
}

// Line draws a one-pixel line from a to b, both endpoints included.
//
// This is Bresenham's algorithm in its canonical all-octant integer form. It
// is deliberately not symmetric in its arguments: swapping a and b can shift
// the line by a pixel. The rule is specified in testdata/README.md and pinned
// by the conformance fixture.
func (c *Canvas) Line(a, b image.Point, i epaper.Ink) {
	idx, ok := c.ink(i)
	if !ok {
		return
	}
	dx, sx := abs(b.X-a.X), sign(b.X-a.X)
	dy, sy := -abs(b.Y-a.Y), sign(b.Y-a.Y)
	err := dx + dy
	x, y := a.X, a.Y
	for {
		c.setIndex(x, y, idx)
		if x == b.X && y == b.Y {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x += sx
		}
		if e2 <= dx {
			err += dx
			y += sy
		}
	}
}

// LineWeight draws a line of the given weight in pixels, both endpoints
// included. A weight of 1 is exactly [Canvas.Line]; below 1 draws nothing.
//
// The rule, specified in testdata/README.md: the union of a weight x weight
// square stamped on every pixel of the 1px Bresenham line. That has two
// consequences worth knowing before using it.
//
// An EVEN weight cannot be centred on a pixel grid, so it sits half a pixel
// right and down of the line. Biasing consistently beats rounding one way at
// one end and the other way at the other; if you want symmetry, use an odd
// weight.
//
// A DIAGONAL reads slightly heavier than an axis-aligned line of the same
// weight, because a square stamp is a constant Chebyshev radius: the band
// across a 45-degree line is about weight*sqrt(2) wide. Fixing that needs a
// distance test in real arithmetic, which is precisely the unportable
// rasteriser this package refuses to depend on — see PLAN §9.8.
//
// In exchange, joins are free: consecutive segments of a polyline share an
// endpoint, and the square stamped there fills the wedge that would otherwise
// be a notch on the outside of the turn. Draw a curve as a sequence of these
// and the corners look after themselves.
func (c *Canvas) LineWeight(a, b image.Point, i epaper.Ink, weight int) {
	idx, ok := c.ink(i)
	if !ok {
		return
	}
	if weight < 1 {
		// Not an error: a caller computing a weight from data can reasonably
		// arrive at zero, and drawing a hairline instead would be inventing
		// an intent they did not express.
		return
	}
	if weight == 1 {
		c.Line(a, b, i)
		return
	}

	// Integer division, so an even weight is biased right and down. See the
	// doc comment: this is the documented behaviour, not a rounding accident.
	lo, hi := (weight-1)/2, weight/2

	dx, sx := abs(b.X-a.X), sign(b.X-a.X)
	dy, sy := -abs(b.Y-a.Y), sign(b.Y-a.Y)
	err := dx + dy
	x, y := a.X, a.Y
	for {
		for py := y - lo; py <= y+hi; py++ {
			for px := x - lo; px <= x+hi; px++ {
				c.setIndex(px, py, idx)
			}
		}
		if x == b.X && y == b.Y {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x += sx
		}
		if e2 <= dx {
			err += dx
			y += sy
		}
	}
}

// Ellipse fills the ellipse inscribed in a rectangle, touching each side at
// exactly one point.
//
// The inside test is integer arithmetic in doubled coordinates, so a
// half-integer centre needs no floating point. See testdata/README.md.
func (c *Canvas) Ellipse(r image.Rectangle, i epaper.Ink) {
	idx, ok := c.ink(i)
	if !ok {
		return
	}
	e, fine := newEllipse(r)
	if !fine {
		return
	}
	for y := e.y0; y <= e.y1; y++ {
		for x := e.x0; x <= e.x1; x++ {
			if e.inside(x, y) {
				c.setIndex(x, y, idx)
			}
		}
	}
}

// StrokeEllipse draws a one-pixel outline of the same ellipse [Canvas.Ellipse]
// fills, leaving the interior untouched.
//
// The outline is derived from the same inside test as the fill — an inside
// pixel with at least one of its four neighbours outside — rather than from a
// second algorithm. There is therefore nothing for the two to disagree about.
func (c *Canvas) StrokeEllipse(r image.Rectangle, i epaper.Ink) {
	idx, ok := c.ink(i)
	if !ok {
		return
	}
	e, fine := newEllipse(r)
	if !fine {
		return
	}
	for y := e.y0; y <= e.y1; y++ {
		for x := e.x0; x <= e.x1; x++ {
			if e.onEdge(x, y) {
				c.setIndex(x, y, idx)
			}
		}
	}
}

// Polygon fills a convex polygon. Points on an edge are drawn, so the polygon
// includes its own outline, and the winding order does not matter.
//
// Convex only: the half-space test this uses is not a general scanline
// rasteriser, and a concave polygon will come out as its convex hull. Fewer
// than three points is an error, reported through [Canvas.Err].
func (c *Canvas) Polygon(pts []image.Point, i epaper.Ink) {
	idx, ok := c.ink(i)
	if !ok {
		return
	}
	if len(pts) < 3 {
		c.fail(fmt.Errorf("render: polygon needs at least 3 points, got %d", len(pts)))
		return
	}

	bounds := image.Rectangle{Min: pts[0], Max: pts[0].Add(image.Pt(1, 1))}
	for _, p := range pts[1:] {
		bounds = bounds.Union(image.Rectangle{Min: p, Max: p.Add(image.Pt(1, 1))})
	}
	bounds = bounds.Intersect(c.img.Rect)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			var pos, neg bool
			for n, a := range pts {
				b := pts[(n+1)%len(pts)]
				// The edge function: positive on one side, negative on
				// the other, zero exactly on the edge.
				e := (b.X-a.X)*(y-a.Y) - (b.Y-a.Y)*(x-a.X)
				switch {
				case e > 0:
					pos = true
				case e < 0:
					neg = true
				}
			}
			// Seeing BOTH signs means the pixel is on the outside of at
			// least one edge. Testing for that, rather than for "all
			// positive", is what makes this independent of the winding
			// order.
			if pos && neg {
				continue
			}
			c.img.Pix[c.img.PixOffset(x, y)] = idx
		}
	}
}

// ellipse holds the inclusive box an ellipse is inscribed in, plus the doubled
// axis lengths its inside test needs.
type ellipse struct {
	x0, y0, x1, y1 int
	ax, ay         int
}

// newEllipse converts a half-open rectangle to the inclusive form the inside
// test works in. It reports false for a rectangle too small to hold one.
func newEllipse(r image.Rectangle) (ellipse, bool) {
	if r.Dx() < 1 || r.Dy() < 1 {
		return ellipse{}, false
	}
	e := ellipse{x0: r.Min.X, y0: r.Min.Y, x1: r.Max.X - 1, y1: r.Max.Y - 1}
	e.ax, e.ay = e.x1-e.x0, e.y1-e.y0
	return e, true
}

// inside reports whether a pixel centre lies within the ellipse:
//
//	dx²·ay² + dy²·ax² <= ax²·ay²
//
// which is the ordinary ellipse equation with every term multiplied by
// (2·ax·ay)², so that a half-integer centre stays in the integers.
func (e ellipse) inside(x, y int) bool {
	dx := 2*x - (e.x0 + e.x1)
	dy := 2*y - (e.y0 + e.y1)
	// A degenerate axis makes the ellipse a line; treat the box as solid.
	if e.ax == 0 || e.ay == 0 {
		return true
	}
	return dx*dx*e.ay*e.ay+dy*dy*e.ax*e.ax <= e.ax*e.ax*e.ay*e.ay
}

// onEdge reports whether a pixel is inside but has a neighbour outside.
func (e ellipse) onEdge(x, y int) bool {
	if !e.inside(x, y) {
		return false
	}
	return !e.inside(x-1, y) || !e.inside(x+1, y) ||
		!e.inside(x, y-1) || !e.inside(x, y+1)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func sign(v int) int {
	if v < 0 {
		return -1
	}
	return 1
}

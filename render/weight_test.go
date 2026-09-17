package render_test

import (
	"image"
	"testing"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/render"
)

// From issue #4: a 2px curve, so a price line reads from across a room.
//
// The rule is specified in testdata/README.md — a w x w square stamped on
// every pixel of the 1px Bresenham line — and these assert it rather than
// whatever the implementation happens to do.

// Weight 1 must be byte-identical to Line. If it is not, every existing
// drawing is at risk from the new code path.
func TestLineWeightOneIsExactlyLine(t *testing.T) {
	for _, ab := range [][4]int{
		{0, 0, 39, 29}, {39, 29, 0, 0}, {0, 15, 39, 15}, {20, 0, 20, 29},
		{0, 0, 39, 1}, {5, 25, 35, 4}, {7, 7, 7, 7},
	} {
		a, b := image.Pt(ab[0], ab[1]), image.Pt(ab[2], ab[3])

		thin := render.NewCanvas(image.Rect(0, 0, 40, 30), fourInk)
		thin.Fill(epaper.White)
		thin.Line(a, b, epaper.Black)

		thick := render.NewCanvas(image.Rect(0, 0, 40, 30), fourInk)
		thick.Fill(epaper.White)
		thick.LineWeight(a, b, epaper.Black, 1)

		if string(thin.Image().Pix) != string(thick.Image().Pix) {
			t.Errorf("LineWeight(%v,%v,1) differs from Line(%v,%v)", a, b, a, b)
		}
	}
}

// A horizontal line of weight w must be exactly w pixels tall, everywhere
// along its length. This is the case a caller can check by eye, so it is the
// one most likely to be noticed if it is wrong.
func TestLineWeightThicknessIsExact(t *testing.T) {
	for _, w := range []int{1, 2, 3, 4, 7} {
		c := render.NewCanvas(image.Rect(0, 0, 40, 30), fourInk)
		c.Fill(epaper.White)
		c.LineWeight(image.Pt(5, 15), image.Pt(34, 15), epaper.Black, w)
		if err := c.Err(); err != nil {
			t.Fatalf("weight %d: %v", w, err)
		}
		for x := 5; x <= 34; x++ {
			n := 0
			for y := range 30 {
				if c.Image().ColorIndexAt(x, y) == 0 {
					n++
				}
			}
			if n != w {
				t.Fatalf("weight %d: column %d is %d px tall, want %d", w, x, n, w)
			}
		}
	}
}

// The documented bias: an even weight sits half a pixel right and down.
// Pinning it means the rule in testdata/README.md and the code cannot drift.
func TestLineWeightEvenBiasIsDocumented(t *testing.T) {
	c := render.NewCanvas(image.Rect(0, 0, 20, 20), fourInk)
	c.Fill(epaper.White)
	c.LineWeight(image.Pt(10, 10), image.Pt(10, 10), epaper.Black, 2)

	var got []image.Point
	for y := range 20 {
		for x := range 20 {
			if c.Image().ColorIndexAt(x, y) == 0 {
				got = append(got, image.Pt(x, y))
			}
		}
	}
	want := []image.Point{{X: 10, Y: 10}, {X: 11, Y: 10}, {X: 10, Y: 11}, {X: 11, Y: 11}}
	if len(got) != len(want) {
		t.Fatalf("a single point at weight 2 covered %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("covered %v, want %v — the even-weight bias moved", got, want)
		}
	}
}

// Weight 3 centres, because an odd width can.
func TestLineWeightOddIsCentred(t *testing.T) {
	c := render.NewCanvas(image.Rect(0, 0, 20, 20), fourInk)
	c.Fill(epaper.White)
	c.LineWeight(image.Pt(10, 10), image.Pt(10, 10), epaper.Black, 3)
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if c.Image().ColorIndexAt(10+dx, 10+dy) != 0 {
				t.Errorf("(%d,%d) not inked", 10+dx, 10+dy)
			}
		}
	}
	if c.Image().ColorIndexAt(10, 10-2) == 0 {
		t.Error("weight 3 reached two pixels above centre; it should span exactly 3")
	}
}

// A polyline must have no notch at the joins — that is the whole reason the
// stamp is a square rather than a perpendicular band.
func TestLineWeightJoinsHaveNoGap(t *testing.T) {
	c := render.NewCanvas(image.Rect(0, 0, 40, 40), fourInk)
	c.Fill(epaper.White)
	pts := []image.Point{{X: 5, Y: 30}, {X: 15, Y: 10}, {X: 25, Y: 25}, {X: 35, Y: 5}}
	for i := 1; i < len(pts); i++ {
		c.LineWeight(pts[i-1], pts[i], epaper.Black, 3)
	}
	if err := c.Err(); err != nil {
		t.Fatalf("Err(): %v", err)
	}
	// Every join's 3x3 neighbourhood must be solid.
	for _, p := range pts[1 : len(pts)-1] {
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				if c.Image().ColorIndexAt(p.X+dx, p.Y+dy) != 0 {
					t.Errorf("join at %v has a gap at (%d,%d)", p, p.X+dx, p.Y+dy)
				}
			}
		}
	}
}

// Clipping, and the degenerate cases, must not panic or spill.
func TestLineWeightEdgeCases(t *testing.T) {
	for _, tc := range []struct {
		name   string
		a, b   image.Point
		weight int
	}{
		{"off the left", image.Pt(-50, 10), image.Pt(10, 10), 5},
		{"entirely outside", image.Pt(-99, -99), image.Pt(-90, -90), 9},
		{"across a corner", image.Pt(-5, -5), image.Pt(45, 35), 4},
		{"zero weight", image.Pt(5, 5), image.Pt(20, 20), 0},
		{"negative weight", image.Pt(5, 5), image.Pt(20, 20), -3},
		{"huge weight", image.Pt(20, 15), image.Pt(21, 16), 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := render.NewCanvas(image.Rect(0, 0, 40, 30), fourInk)
			c.LineWeight(tc.a, tc.b, epaper.Black, tc.weight)
			if err := c.Err(); err != nil {
				t.Errorf("Err() = %v; clipping is not a failure", err)
			}
		})
	}
}

// A weight below 1 draws nothing rather than guessing at an intent.
func TestLineWeightBelowOneDrawsNothing(t *testing.T) {
	for _, w := range []int{0, -1, -100} {
		c := render.NewCanvas(image.Rect(0, 0, 20, 20), fourInk)
		c.Fill(epaper.White)
		c.LineWeight(image.Pt(2, 2), image.Pt(17, 17), epaper.Black, w)
		if n := countInk(c, 0); n != 0 {
			t.Errorf("weight %d inked %d pixels, want 0", w, n)
		}
	}
}

// An ink the palette lacks is an error, as everywhere else.
func TestLineWeightMissingInk(t *testing.T) {
	c := render.NewCanvas(image.Rect(0, 0, 20, 20), fourInk)
	c.LineWeight(image.Pt(2, 2), image.Pt(17, 17), epaper.Green, 3)
	if c.Err() == nil {
		t.Error("drawing in an ink the palette lacks must be reported")
	}
}

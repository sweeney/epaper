package render_test

import (
	"image"
	"testing"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/internal/golden"
	"github.com/sweeney/epaper/render"
)

// Golden tests cover what the conformance fixture cannot: anything with text
// in it. Pillow and x/image/font rasterise glyphs differently, so these are
// compared against our own reviewed output, not the vendor's.
//
// Regenerate with `make golden`, then LOOK at the diff.

func TestGoldenText(t *testing.T) {
	c := render.NewCanvas(image.Rect(0, 0, 400, 120), fourInk)
	c.Fill(epaper.White)

	// A legibility ladder. The bench established 10px as the smallest
	// readable size on this panel and 8px as not readable; this golden is
	// where that stays visible to anyone reviewing a change.
	y := 4
	for _, size := range []int{8, 10, 12, 16, 24} {
		c.Text(image.Pt(4, y), "Hamburgefonstiv 0123", face(t, size), epaper.Black)
		y += render.LineHeight(face(t, size)) + 2
	}
	if err := c.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
	golden.Assert(t, "text-ladder", c.Image())
}

func TestGoldenTextFitted(t *testing.T) {
	c := render.NewCanvas(image.Rect(0, 0, 400, 140), fourInk)
	c.Fill(epaper.White)

	// The same string in boxes of decreasing width. Each must be drawn whole
	// and inside its box — this is the picture of the bug TextFitted exists
	// to prevent, not happening.
	for i, w := range []int{200, 150, 110, 75} {
		box := image.Rect(4, 4+i*34, 4+w, 32+i*34)
		c.StrokeRect(box, epaper.Red)
		c.TextFitted(box.Inset(2), "RED/YELLOW", testFamily, epaper.Black)
	}
	if err := c.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
	golden.Assert(t, "text-fitted", c.Image())
}

func TestGoldenPrimitives(t *testing.T) {
	c := render.NewCanvas(image.Rect(0, 0, 200, 120), fourInk)
	c.Fill(epaper.White)

	c.Rect(image.Rect(4, 4, 60, 40), epaper.Red)
	c.StrokeRect(image.Rect(4, 4, 60, 40), epaper.Black)
	c.Ellipse(image.Rect(70, 4, 130, 40), epaper.Yellow)
	c.StrokeEllipse(image.Rect(70, 4, 130, 40), epaper.Black)
	c.Polygon([]image.Point{{140, 38}, {168, 6}, {196, 38}}, epaper.Red)

	// A fan of lines through every octant, to catch a Bresenham that is wrong
	// in only some directions.
	centre := image.Pt(100, 70)
	for _, d := range []image.Point{
		{44, 0}, {44, 18}, {26, 26}, {0, 26}, {-26, 26}, {-44, 18},
		{-44, 0}, {-44, -18}, {-26, -26}, {0, -26}, {26, -26}, {44, -18},
	} {
		c.Line(centre, centre.Add(d), epaper.Black)
	}

	c.Checker(image.Rect(4, 100, 64, 116), epaper.Yellow, epaper.Red, 2)
	for x := 70; x < 196; x++ {
		c.Dither(image.Rect(x, 100, x+1, 116), epaper.Black, epaper.White,
			float64(x-70)/float64(196-70-1))
	}

	if err := c.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
	golden.Assert(t, "primitives", c.Image())
}

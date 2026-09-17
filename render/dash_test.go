package render_test

import (
	"image"
	"testing"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/render"
)

// From issue #5. The rule is in testdata/README.md: the Bresenham line, with
// pixels inked for `on` steps then skipped for `off`, starting inked at a.

func TestDashPattern(t *testing.T) {
	c := render.NewCanvas(image.Rect(0, 0, 24, 3), fourInk)
	c.Fill(epaper.White)
	c.Dash(image.Pt(0, 1), image.Pt(19, 1), epaper.Black, 3, 2)
	if err := c.Err(); err != nil {
		t.Fatalf("Err(): %v", err)
	}

	// 3 on, 2 off, repeating, starting at a.
	want := "###..###..###..###.."
	var got []byte
	for x := range 20 {
		if c.Image().ColorIndexAt(x, 1) == 0 {
			got = append(got, '#')
		} else {
			got = append(got, '.')
		}
	}
	if string(got) != want {
		t.Errorf("dash 3/2 =\n  %s\nwant\n  %s", got, want)
	}
}

// off <= 0 is a solid line, not an error and not an infinite loop: a caller
// computing a gap from data can reasonably arrive at zero.
func TestDashWithNoGapIsASolidLine(t *testing.T) {
	for _, off := range []int{0, -1, -50} {
		dashed := render.NewCanvas(image.Rect(0, 0, 30, 30), fourInk)
		dashed.Fill(epaper.White)
		dashed.Dash(image.Pt(2, 2), image.Pt(27, 20), epaper.Black, 4, off)

		solid := render.NewCanvas(image.Rect(0, 0, 30, 30), fourInk)
		solid.Fill(epaper.White)
		solid.Line(image.Pt(2, 2), image.Pt(27, 20), epaper.Black)

		if string(dashed.Image().Pix) != string(solid.Image().Pix) {
			t.Errorf("off=%d did not draw a solid line", off)
		}
	}
}

// on <= 0 would draw nothing for ever. It must draw nothing, once.
func TestDashWithNoInkDrawsNothing(t *testing.T) {
	for _, on := range []int{0, -1} {
		c := render.NewCanvas(image.Rect(0, 0, 30, 30), fourInk)
		c.Fill(epaper.White)
		c.Dash(image.Pt(2, 2), image.Pt(27, 20), epaper.Black, on, 3)
		if n := countInk(c, 0); n != 0 {
			t.Errorf("on=%d inked %d pixels, want 0", on, n)
		}
		if err := c.Err(); err != nil {
			t.Errorf("on=%d: %v", on, err)
		}
	}
}

// The phase counts STEPS along the line, not pixels of screen distance, so a
// diagonal dashes at the same rhythm as an axis-aligned one. Stating it
// because the alternative — equal screen lengths — is what a reader might
// assume, and it would need real arithmetic.
func TestDashPhaseCountsSteps(t *testing.T) {
	c := render.NewCanvas(image.Rect(0, 0, 24, 24), fourInk)
	c.Fill(epaper.White)
	c.Dash(image.Pt(0, 0), image.Pt(19, 19), epaper.Black, 3, 2)

	var got []byte
	for i := range 20 {
		if c.Image().ColorIndexAt(i, i) == 0 {
			got = append(got, '#')
		} else {
			got = append(got, '.')
		}
	}
	if string(got) != "###..###..###..###.." {
		t.Errorf("diagonal dash = %s, want the same rhythm as a straight one", got)
	}
}

func TestDashEdgeCases(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b image.Point
	}{
		{"single point", image.Pt(5, 5), image.Pt(5, 5)},
		{"off canvas", image.Pt(-40, 5), image.Pt(-20, 5)},
		{"across a corner", image.Pt(-5, -5), image.Pt(35, 35)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := render.NewCanvas(image.Rect(0, 0, 30, 30), fourInk)
			c.Fill(epaper.White)
			c.Dash(tc.a, tc.b, epaper.Black, 2, 2)
			if err := c.Err(); err != nil {
				t.Errorf("Err() = %v; clipping is not a failure", err)
			}
		})
	}
}

func TestDashMissingInk(t *testing.T) {
	c := render.NewCanvas(image.Rect(0, 0, 30, 30), fourInk)
	c.Dash(image.Pt(2, 2), image.Pt(27, 20), epaper.Green, 3, 2)
	if c.Err() == nil {
		t.Error("an ink the palette lacks must be reported")
	}
}

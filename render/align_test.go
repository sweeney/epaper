package render_test

import (
	"image"
	"testing"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/render"
	"golang.org/x/image/font/basicfont"
)

// From issue #5: Canvas.Text takes a top-left point and that is the whole of
// positioning, so every dashboard writes the same six-line textRight. The
// overflow axis has three answers now; the placement axis had one.
//
// basicfont is 7px per glyph, so the arithmetic is checkable by hand.
func TestTextAligned(t *testing.T) {
	const w = 7 * 5 // "ABCDE"
	box := image.Rect(10, 0, 110, 13)

	for _, tc := range []struct {
		align render.Align
		wantX int
	}{
		{render.AlignLeft, 10},
		{render.AlignCentre, 10 + (100-w)/2},
		{render.AlignRight, 110 - w},
	} {
		c := render.NewCanvas(image.Rect(0, 0, 120, 13), fourInk)
		c.Fill(epaper.White)
		c.TextAligned(box, "ABCDE", basicfont.Face7x13, epaper.Black, tc.align)
		if err := c.Err(); err != nil {
			t.Fatalf("Err(): %v", err)
		}

		// The leftmost inked column is where the text starts. Comparing
		// against a plain Text call at the expected point is stronger than
		// eyeballing a number: it asserts the two produce the same pixels.
		want := render.NewCanvas(image.Rect(0, 0, 120, 13), fourInk)
		want.Fill(epaper.White)
		want.Text(image.Pt(tc.wantX, 0), "ABCDE", basicfont.Face7x13, epaper.Black)

		if string(c.Image().Pix) != string(want.Image().Pix) {
			t.Errorf("align %v did not place the text at x=%d", tc.align, tc.wantX)
		}
	}
}

// Text wider than its box cannot be aligned in any meaningful sense. It must
// start at the box's left edge rather than running off to the left, which is
// what a naive right-align does and what makes a clock vanish off the panel.
func TestTextAlignedClampsOverlongText(t *testing.T) {
	box := image.Rect(20, 0, 50, 13) // 30px, far too narrow
	for _, align := range []render.Align{render.AlignLeft, render.AlignCentre, render.AlignRight} {
		c := render.NewCanvas(image.Rect(0, 0, 120, 13), fourInk)
		c.Fill(epaper.White)
		c.TextAligned(box, "ABCDEFGHIJ", basicfont.Face7x13, epaper.Black, align)

		leftmost := 120
		for x := range 120 {
			for y := range 13 {
				if c.Image().ColorIndexAt(x, y) == 0 {
					if x < leftmost {
						leftmost = x
					}
				}
			}
		}
		if leftmost < box.Min.X {
			t.Errorf("align %v put ink at x=%d, left of the box at %d", align, leftmost, box.Min.X)
		}
	}
}

// It must compose with truncation, which is the combination a dashboard
// actually wants: fixed size, right-aligned, cut if too long.
func TestTextAlignedComposesWithTruncation(t *testing.T) {
	box := image.Rect(0, 0, 49, 13)
	c := render.NewCanvas(image.Rect(0, 0, 60, 13), fourInk)
	c.Fill(epaper.White)

	s := render.TruncateText("ABCDEFGH", basicfont.Face7x13, box.Dx())
	c.TextAligned(box, s, basicfont.Face7x13, epaper.Black, render.AlignRight)
	if err := c.Err(); err != nil {
		t.Fatalf("Err(): %v", err)
	}
	if s != "ABCD..." {
		t.Fatalf("truncation gave %q", s)
	}
	// Right-aligned in a 49px box, 7 glyphs at 7px = 49, so it starts at 0.
	if c.Image().ColorIndexAt(0, 3) != 0 && c.Image().ColorIndexAt(1, 3) != 0 {
		t.Error("nothing drawn at the left edge of a box the text exactly fills")
	}
}

func TestTextAlignedEdgeCases(t *testing.T) {
	c := render.NewCanvas(image.Rect(0, 0, 60, 20), fourInk)
	c.Fill(epaper.White)

	// Empty string draws nothing and is not an error.
	c.TextAligned(image.Rect(0, 0, 50, 13), "", basicfont.Face7x13, epaper.Black, render.AlignRight)
	if err := c.Err(); err != nil {
		t.Errorf("empty string: %v", err)
	}
	if countInk(c, 0) != 0 {
		t.Error("empty string drew something")
	}

	// Nil face is an error, like everywhere else.
	c.TextAligned(image.Rect(0, 0, 50, 13), "x", nil, epaper.Black, render.AlignRight)
	if c.Err() == nil {
		t.Error("nil face must be reported")
	}
}

func TestTextAlignedMissingInk(t *testing.T) {
	c := render.NewCanvas(image.Rect(0, 0, 60, 20), fourInk)
	c.TextAligned(image.Rect(0, 0, 50, 13), "x", basicfont.Face7x13, epaper.Green, render.AlignRight)
	if c.Err() == nil {
		t.Error("an ink the palette lacks must be reported")
	}
}

// An unknown alignment is a programming error, not a silent left-align.
func TestTextAlignedRejectsAnUnknownAlignment(t *testing.T) {
	c := render.NewCanvas(image.Rect(0, 0, 60, 20), fourInk)
	c.TextAligned(image.Rect(0, 0, 50, 13), "x", basicfont.Face7x13, epaper.Black, render.Align(99))
	if c.Err() == nil {
		t.Error("an unknown Align must be reported rather than guessed at")
	}
}

func TestAlignString(t *testing.T) {
	for _, tc := range []struct {
		a    render.Align
		want string
	}{
		{render.AlignLeft, "left"},
		{render.AlignCentre, "centre"},
		{render.AlignRight, "right"},
	} {
		if got := tc.a.String(); got != tc.want {
			t.Errorf("Align(%d).String() = %q, want %q", tc.a, got, tc.want)
		}
	}
	if render.Align(99).String() == "" {
		t.Error("an unknown Align must still print something")
	}
}

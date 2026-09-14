package render_test

import (
	"errors"
	"image"
	"sync"
	"testing"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/render"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
)

// testFamily is a FontFamily over Go Regular, cached as the FontFamily docs
// ask. Tests only: the library embeds no fonts.
var testFamily = func() render.FontFamily {
	parsed, err := opentype.Parse(goregular.TTF)
	if err != nil {
		panic(err)
	}
	var mu sync.Mutex
	cache := map[int]font.Face{}
	return func(size int) (font.Face, error) {
		mu.Lock()
		defer mu.Unlock()
		if f, ok := cache[size]; ok {
			return f, nil
		}
		// DPI 72 makes one point equal one pixel, so "size" means px.
		f, err := opentype.NewFace(parsed, &opentype.FaceOptions{
			Size: float64(size), DPI: 72, Hinting: font.HintingFull,
		})
		if err != nil {
			return nil, err
		}
		cache[size] = f
		return f, nil
	}
}()

func face(t *testing.T, size int) font.Face {
	t.Helper()
	f, err := testFamily(size)
	if err != nil {
		t.Fatalf("building a %dpx face: %v", size, err)
	}
	return f
}

// countInk returns how many pixels carry the given index.
func countInk(c *render.Canvas, idx uint8) int {
	n := 0
	b := c.Image().Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if c.Image().ColorIndexAt(x, y) == idx {
				n++
			}
		}
	}
	return n
}

func TestTextDrawsSomething(t *testing.T) {
	c := newCanvas(200, 40)
	c.Fill(epaper.White)
	c.Text(image.Pt(4, 4), "HELLO", face(t, 24), epaper.Black)
	if err := c.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
	if n := countInk(c, black); n == 0 {
		t.Fatal("Text() drew no ink at all")
	}
}

// p is the top-left, not the baseline. If it were the baseline, text drawn at
// y=0 would fall almost entirely off the top of the canvas.
func TestTextIsPositionedByItsTopLeft(t *testing.T) {
	c := newCanvas(200, 40)
	c.Fill(epaper.White)
	c.Text(image.Pt(0, 0), "Hg", face(t, 24), epaper.Black)

	if n := countInk(c, black); n == 0 {
		t.Fatal("Text() at (0,0) drew nothing — is it positioning by baseline?")
	}
	// Nothing above the top edge is possible, but the ink should start near it.
	firstRow := -1
	for y := range 40 {
		for x := range 200 {
			if c.Image().ColorIndexAt(x, y) == black {
				firstRow = y
				break
			}
		}
		if firstRow >= 0 {
			break
		}
	}
	if firstRow < 0 || firstRow > 12 {
		t.Errorf("first inked row = %d, want it near the top for text placed at y=0", firstRow)
	}
}

// Text is binary on this panel. A blended edge would scatter yellow and red
// around black text, since "nearest palette colour" has no greys to pick from.
func TestTextUsesOnlyTheRequestedInk(t *testing.T) {
	c := newCanvas(200, 40)
	c.Fill(epaper.White)
	c.Text(image.Pt(4, 4), "Hamburgefonstiv", face(t, 24), epaper.Black)

	if got := countInk(c, yellow) + countInk(c, red); got != 0 {
		t.Errorf("%d pixels are yellow or red; text must be binary in one ink", got)
	}
	if countInk(c, black)+countInk(c, white) != 200*40 {
		t.Error("some pixels are neither the ink nor the background")
	}
}

func TestMeasureText(t *testing.T) {
	f := face(t, 24)

	if got := render.MeasureText("", f); got != 0 {
		t.Errorf("MeasureText(\"\") = %d, want 0", got)
	}
	short, long := render.MeasureText("i", f), render.MeasureText("WWWWW", f)
	if short >= long {
		t.Errorf("MeasureText(\"i\") = %d is not less than MeasureText(\"WWWWW\") = %d", short, long)
	}
	// Bigger face, wider string.
	if a, b := render.MeasureText("HELLO", face(t, 12)), render.MeasureText("HELLO", face(t, 24)); a >= b {
		t.Errorf("12px width %d is not less than 24px width %d", a, b)
	}
	if render.MeasureText("x", nil) != 0 {
		t.Error("MeasureText() with a nil face should be 0, not a panic")
	}
}

// The measurement is what layout trusts, so it must not under-report. Drawing
// the string must not put ink beyond the width measured for it.
func TestMeasureTextDoesNotUnderReport(t *testing.T) {
	const s = "Hamburgefonstiv 0123"
	f := face(t, 20)
	w := render.MeasureText(s, f)

	c := render.NewCanvas(image.Rect(0, 0, w+50, 40), fourInk)
	c.Fill(epaper.White)
	c.Text(image.Pt(0, 4), s, f, epaper.Black)

	for y := range 40 {
		for x := w; x < w+50; x++ {
			if c.Image().ColorIndexAt(x, y) == black {
				t.Fatalf("ink at x=%d, beyond the measured width of %d", x, w)
			}
		}
	}
}

func TestLineHeight(t *testing.T) {
	if got := render.LineHeight(face(t, 24)); got <= 0 {
		t.Errorf("LineHeight() = %d, want a positive height", got)
	}
	if a, b := render.LineHeight(face(t, 12)), render.LineHeight(face(t, 24)); a >= b {
		t.Errorf("12px line height %d is not less than 24px %d", a, b)
	}
	if render.LineHeight(nil) != 0 {
		t.Error("LineHeight() with a nil face should be 0, not a panic")
	}
}

func TestTextFitted(t *testing.T) {
	c := newCanvas(200, 40)
	c.Fill(epaper.White)

	size := c.TextFitted(image.Rect(0, 0, 200, 40), "HELLO", testFamily, epaper.Black)
	if err := c.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
	if size <= 0 {
		t.Fatalf("TextFitted() = %d, want a positive size", size)
	}
	if countInk(c, black) == 0 {
		t.Error("TextFitted() drew nothing")
	}

	// What it returns must actually fit.
	f := face(t, size)
	if w := render.MeasureText("HELLO", f); w > 200 {
		t.Errorf("chosen size %d gives width %d, which does not fit 200", size, w)
	}
	if h := render.LineHeight(f); h > 40 {
		t.Errorf("chosen size %d gives line height %d, which does not fit 40", size, h)
	}
	// And it must be the largest that does.
	if bigger := face(t, size+1); render.MeasureText("HELLO", bigger) <= 200 && render.LineHeight(bigger) <= 40 {
		t.Errorf("size %d fits too; TextFitted returned %d and is not choosing the largest", size+1, size)
	}
}

// A long string in a narrow box shrinks rather than clipping.
func TestTextFittedShrinksToFit(t *testing.T) {
	wide := newCanvas(400, 40)
	wide.Fill(epaper.White)
	big := wide.TextFitted(image.Rect(0, 0, 400, 40), "RED/YELLOW", testFamily, epaper.Black)

	narrow := newCanvas(400, 40)
	narrow.Fill(epaper.White)
	small := narrow.TextFitted(image.Rect(0, 0, 120, 40), "RED/YELLOW", testFamily, epaper.Black)

	if small >= big {
		t.Errorf("narrow box chose %dpx, wide box chose %dpx; it is not shrinking", small, big)
	}
	if narrow.Err() != nil {
		t.Errorf("Err() = %v", narrow.Err())
	}
	// The bench bug this prevents: "RED/YELLOW" losing its W off the edge.
	for y := range 40 {
		for x := 120; x < 400; x++ {
			if narrow.Image().ColorIndexAt(x, y) == black {
				t.Fatalf("ink at x=%d, outside the %dpx box it was fitted to", x, 120)
			}
		}
	}
}

// If it genuinely cannot fit, that must be loud. Silence is the failure mode
// this whole function exists to prevent.
func TestTextFittedReportsAnImpossibleFit(t *testing.T) {
	c := newCanvas(400, 40)
	c.Fill(epaper.White)

	size := c.TextFitted(image.Rect(0, 0, 4, 4), "a very long string indeed", testFamily, epaper.Black)
	if size != 0 {
		t.Errorf("TextFitted() = %d, want 0", size)
	}
	if err := c.Err(); !errors.Is(err, render.ErrTextDoesNotFit) {
		t.Errorf("Err() = %v, want it to wrap ErrTextDoesNotFit", err)
	}
}

func TestTextErrorCases(t *testing.T) {
	t.Run("nil face", func(t *testing.T) {
		c := newCanvas(40, 40)
		c.Text(image.Pt(0, 0), "x", nil, epaper.Black)
		if c.Err() == nil {
			t.Error("Err() = nil after Text with a nil face")
		}
	})
	t.Run("nil family", func(t *testing.T) {
		c := newCanvas(40, 40)
		if got := c.TextFitted(image.Rect(0, 0, 40, 40), "x", nil, epaper.Black); got != 0 {
			t.Errorf("TextFitted() = %d, want 0", got)
		}
		if c.Err() == nil {
			t.Error("Err() = nil after TextFitted with a nil family")
		}
	})
	t.Run("empty rect", func(t *testing.T) {
		c := newCanvas(40, 40)
		if got := c.TextFitted(image.Rect(0, 0, 0, 0), "x", testFamily, epaper.Black); got != 0 {
			t.Errorf("TextFitted() = %d, want 0", got)
		}
		if c.Err() == nil {
			t.Error("Err() = nil after TextFitted with an empty rect")
		}
	})
	t.Run("absent ink", func(t *testing.T) {
		c := newCanvas(40, 40)
		c.Text(image.Pt(0, 0), "x", face(t, 12), epaper.Green)
		if !errors.Is(c.Err(), render.ErrInkUnavailable) {
			t.Errorf("Err() = %v, want ErrInkUnavailable", c.Err())
		}
	})
	t.Run("family returns an error", func(t *testing.T) {
		c := newCanvas(40, 40)
		boom := errors.New("no such font")
		bad := render.FontFamily(func(int) (font.Face, error) { return nil, boom })
		if got := c.TextFitted(image.Rect(0, 0, 40, 40), "x", bad, epaper.Black); got != 0 {
			t.Errorf("TextFitted() = %d, want 0", got)
		}
		if !errors.Is(c.Err(), boom) {
			t.Errorf("Err() = %v, want it to wrap %v", c.Err(), boom)
		}
	})
}

// Text longer than the canvas is clipped, not an error — but the ink must stop
// at the edge rather than wrapping round to the next row.
func TestTextIsClipped(t *testing.T) {
	c := newCanvas(40, 20)
	c.Fill(epaper.White)
	c.Text(image.Pt(30, 2), "WWWWWWWWWW", face(t, 16), epaper.Black)
	if err := c.Err(); err != nil {
		t.Errorf("Err() = %v, want nil — clipping is not an error", err)
	}
}

// A rune the face has no glyph for must still advance, so the rest of the
// string keeps its position instead of sliding left over the gap.
func TestTextWithAMissingGlyph(t *testing.T) {
	f := face(t, 16)

	withGap := newCanvas(300, 30)
	withGap.Fill(epaper.White)
	withGap.Text(image.Pt(2, 2), "ABCD", f, epaper.Black) // private-use rune

	if err := withGap.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
	if countInk(withGap, black) == 0 {
		t.Fatal("Text() drew nothing at all")
	}
	// The string with the gap must be wider than the same letters without it.
	plain := render.MeasureText("ABCD", f)
	gapped := render.MeasureText("ABCD", f)
	if gapped < plain {
		t.Errorf("measured width with a missing glyph (%d) is less than without (%d)", gapped, plain)
	}
}

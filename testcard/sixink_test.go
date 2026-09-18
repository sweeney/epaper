package testcard

import (
	"fmt"
	"image"
	"image/color"
	"slices"
	"testing"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/render"
)

// sixInk is a Spectra 6 palette in the E640's order. Declared here rather than
// imported so this package's tests stay independent of any driver, like
// fourInk above.
var sixInk = epaper.Palette{
	{Ink: epaper.Black, RGB: color.RGBA{0, 0, 0, 255}},
	{Ink: epaper.White, RGB: color.RGBA{255, 255, 255, 255}},
	{Ink: epaper.Yellow, RGB: color.RGBA{208, 190, 71, 255}},
	{Ink: epaper.Red, RGB: color.RGBA{156, 72, 75, 255}},
	{Ink: epaper.Blue, RGB: color.RGBA{61, 59, 94, 255}},
	{Ink: epaper.Green, RGB: color.RGBA{58, 91, 70, 255}},
}

func drawSix(t *testing.T) *render.Canvas {
	t.Helper()
	c := render.NewCanvas(image.Rect(0, 0, 600, 400), sixInk)
	Draw(c, Options{Lines: []string{"Spectra 6 4.0 600 x 400 (E640)", "600x400 6-ink"}})
	if err := c.Err(); err != nil {
		t.Fatalf("Err(): %v", err)
	}
	return c
}

// A card that proves a six-ink panel must actually put all six inks on it.
// Drawn in the four-ink card's colours, blue and green would go untested.
func TestSixInkCardUsesEveryInk(t *testing.T) {
	c := drawSix(t)
	for i, e := range sixInk {
		if countIndex(c, uint8(i)) == 0 {
			t.Errorf("the card uses no %s at all", e.Ink)
		}
	}
}

// With blue and green available, the side ladders are Test Card F's own
// colour bars, in their own luminance order. Cyan and magenta are not inks on
// any panel here, so they are mixed, as orange and olive are on four inks.
func TestSixInkLaddersAreTheColourBars(t *testing.T) {
	want := []string{"white", "yellow", "cyan", "green", "magenta", "red", "blue", "black"}
	if got := stepNames(ladderFor(sixInk)); !slices.Equal(got, want) {
		t.Errorf("six-ink ladder = %v, want %v", got, want)
	}
}

// Without them, nothing changes: the four-ink ladder is the one reviewed on
// both red/yellow panels, and its goldens must stand.
func TestFourInkLaddersAreUnchanged(t *testing.T) {
	want := []string{"white", "yellow", "orange", "red", "olive", "dark red", "black"}
	if got := stepNames(ladderFor(fourInk)); !slices.Equal(got, want) {
		t.Errorf("four-ink ladder = %v, want %v", got, want)
	}
	// One of the two is not enough: a palette with blue and no green gets
	// the four-ink ladder, not a colour-bar ladder with a hole in it.
	blueOnly := append(slices.Clone(fourInk), epaper.Entry{Ink: epaper.Blue, RGB: color.RGBA{0, 0, 255, 255}})
	if got := stepNames(ladderFor(blueOnly)); !slices.Equal(got, want) {
		t.Errorf("blue-without-green ladder = %v, want the four-ink one", got)
	}
}

// Every step of the six-ink ladder must be drawable on the palette it was
// chosen for — a step that names an ink the panel lacks poisons the canvas.
func TestSixInkLadderDrawsCleanly(t *testing.T) {
	for _, step := range ladderFor(sixInk) {
		c := render.NewCanvas(image.Rect(0, 0, 8, 8), sixInk)
		step.paint(c, c.Bounds())
		if err := c.Err(); err != nil {
			t.Errorf("step %q: %v", step.name, err)
		}
	}
}

func TestSixInkCircleStaysInside(t *testing.T) {
	got := drawSix(t).Image()

	plain := render.NewCanvas(image.Rect(0, 0, 600, 400), sixInk)
	drawWithoutCircle(plain)
	if err := plain.Err(); err != nil {
		t.Fatalf("Err(): %v", err)
	}
	l := newLayout(600, 400)
	for y := range 400 {
		for x := range 600 {
			if got.ColorIndexAt(x, y) == plain.Image().ColorIndexAt(x, y) {
				continue
			}
			if dx, dy := x-l.circleX, y-l.circleY; dx*dx+dy*dy > l.circleR*l.circleR {
				t.Fatalf("circle content escaped at (%d,%d)", x, y)
			}
		}
	}
}

func stepNames(steps []ladder) []string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.name
	}
	return out
}

// The conformance pattern is fixed at 400x300: it is the vendor oracle's size,
// and scaling it would break the byte comparison. On a bigger panel the rest
// of the glass must not be left blank white, because that reads as a pattern
// drawn too small — which is exactly how it was reported the first time it
// went on the Impression. So the margin is marked, and the pattern itself is
// untouched.
func TestConformanceMarksTheAreaOutsideThePattern(t *testing.T) {
	small := render.NewCanvas(image.Rect(0, 0, 400, 300), sixInk)
	DrawConformance(small)
	big := render.NewCanvas(image.Rect(0, 0, 600, 400), sixInk)
	DrawConformance(big)
	for _, c := range []*render.Canvas{small, big} {
		if err := c.Err(); err != nil {
			t.Fatalf("Err(): %v", err)
		}
	}

	for y := range 300 {
		for x := range 400 {
			if big.Image().ColorIndexAt(x, y) != small.Image().ColorIndexAt(x, y) {
				t.Fatalf("(%d,%d) differs from the 400x300 pattern; the oracle region must not change", x, y)
			}
		}
	}

	white, _ := sixInk.Index(epaper.White)
	for _, r := range []image.Rectangle{image.Rect(401, 0, 600, 400), image.Rect(0, 301, 600, 400)} {
		blank := true
		for y := r.Min.Y; y < r.Max.Y && blank; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				if big.Image().ColorIndexAt(x, y) != white {
					blank = false
					break
				}
			}
		}
		if blank {
			t.Errorf("the margin %v is blank white; it reads as a pattern drawn too small", r)
		}
	}
}

// The heading is centred in the disc. It was drawn at the left of a box as
// wide as the disc, which looked centred wherever the fitted size happened to
// fill that box — and plainly did not on the Impression, where the height
// caps the size first. Found on the glass, 2026-09-25.
//
// The heading is the only red in the top half of the disc, so its ink bounds
// are easy to find without reaching into the drawing.
func TestHeadingIsCentredInTheDisc(t *testing.T) {
	for _, tc := range []struct {
		w, h int
		p    epaper.Palette
	}{
		{600, 400, sixInk},
		{400, 300, fourInk},
		{800, 480, fourInk}, // a 7.3" geometry, to show it is not one size
	} {
		t.Run(fmt.Sprintf("%dx%d", tc.w, tc.h), func(t *testing.T) {
			c := render.NewCanvas(image.Rect(0, 0, tc.w, tc.h), tc.p)
			Draw(c, Options{})
			if err := c.Err(); err != nil {
				t.Fatalf("Err(): %v", err)
			}
			l := newLayout(tc.w, tc.h)
			red, _ := tc.p.Index(epaper.Red)

			minX, maxX := tc.w, -1
			for y := l.circleY - l.circleR; y < l.circleY-l.circleR/2; y++ {
				for x := l.circleX - l.circleR; x <= l.circleX+l.circleR; x++ {
					if dx, dy := x-l.circleX, y-l.circleY; dx*dx+dy*dy >= l.circleR*l.circleR {
						continue
					}
					if c.Image().ColorIndexAt(x, y) == red {
						minX, maxX = min(minX, x), max(maxX, x)
					}
				}
			}
			if maxX < 0 {
				t.Fatal("no heading found in the top of the disc")
			}
			// Two pixels of slack: a glyph's ink need not sit symmetrically
			// in its advance, and an odd leftover cannot split evenly.
			if off := (minX+maxX)/2 - l.circleX; off < -2 || off > 2 {
				t.Errorf("heading spans x %d..%d, centred %d px off the disc's centre %d",
					minX, maxX, off, l.circleX)
			}
		})
	}
}

package testcard_test

import (
	"image"
	"image/color"
	"testing"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/internal/golden"
	"github.com/sweeney/epaper/internal/testcard"
	"github.com/sweeney/epaper/render"
	"golang.org/x/image/font/gofont/goregular"
)

var fourInk = epaper.Palette{
	{Ink: epaper.Black, RGB: color.RGBA{0, 0, 0, 255}},
	{Ink: epaper.White, RGB: color.RGBA{255, 255, 255, 255}},
	{Ink: epaper.Yellow, RGB: color.RGBA{235, 205, 40, 255}},
	{Ink: epaper.Red, RGB: color.RGBA{190, 45, 40, 255}},
}

func draw(t *testing.T, withFonts bool) *render.Canvas {
	t.Helper()
	c := render.NewCanvas(image.Rect(0, 0, 400, 300), fourInk)

	var fonts render.FontFamily
	if withFonts {
		var err error
		if fonts, err = testcard.Fonts(goregular.TTF); err != nil {
			t.Fatalf("Fonts(): %v", err)
		}
	}
	// A fixed note, so the golden does not change with the clock.
	testcard.Draw(c, fonts, "Red/Yellow wHAT (JD79668)", "400x300 4-ink")
	if err := c.Err(); err != nil {
		t.Fatalf("Err(): %v", err)
	}
	return c
}

// M4's acceptance: the test card renders to a golden PNG, entirely on a
// laptop, with no hardware anywhere near it.
func TestGoldenTestCard(t *testing.T) {
	golden.Assert(t, "testcard", draw(t, true).Image())
}

// Fonts are the caller's to supply, so the card has to survive not getting
// any. Every diagnostic element still works; only the labels are lost.
func TestDrawsWithoutFonts(t *testing.T) {
	c := draw(t, false)
	if used := inksUsed(c); len(used) != 4 {
		t.Errorf("used %d inks without fonts, want all 4: %v", len(used), used)
	}
}

// The whole point of the card is that it exercises everything. If a change
// left one ink unused, the card would stop proving the panel can show it.
func TestUsesAllFourInks(t *testing.T) {
	c := draw(t, true)
	counts := map[uint8]int{}
	b := c.Image().Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			counts[c.Image().ColorIndexAt(x, y)]++
		}
	}
	for idx, name := range map[uint8]string{0: "black", 1: "white", 2: "yellow", 3: "red"} {
		if counts[idx] == 0 {
			t.Errorf("the card uses no %s at all", name)
		}
	}
	t.Logf("ink usage: black=%d white=%d yellow=%d red=%d",
		counts[0], counts[1], counts[2], counts[3])
}

// It must pack to exactly what the panel expects, or it is not an acceptance
// test for anything.
func TestCardPacks(t *testing.T) {
	got, err := epaper.Pack(draw(t, true).Image())
	if err != nil {
		t.Fatalf("Pack(): %v", err)
	}
	if len(got) != 30000 {
		t.Errorf("packed to %d bytes, want 30000", len(got))
	}
}

func inksUsed(c *render.Canvas) []uint8 {
	seen := map[uint8]bool{}
	b := c.Image().Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			seen[c.Image().ColorIndexAt(x, y)] = true
		}
	}
	var out []uint8
	for k := range seen {
		out = append(out, k)
	}
	return out
}

// Nothing drawn for the circle may escape it.
//
// The circle is the card's one free-form region, and everything in it is
// positioned from the circle's own geometry. Eyeballing the golden is how the
// overflow was spotted; this is how it stays spotted.
func TestCircleContentStaysInsideTheCircle(t *testing.T) {
	// The card's background is a 50% dither, so "escaped" cannot just mean
	// "a black pixel outside the circle". Draw the card twice, once with the
	// circle contents and once without, and compare: any pixel outside the
	// disc that the two disagree about was put there by the circle.
	withCircle := draw(t, true).Image()

	plain := render.NewCanvas(image.Rect(0, 0, 400, 300), fourInk)
	testcard.DrawWithoutCircle(plain, "Red/Yellow wHAT (JD79668)", "400x300 4-ink")
	if err := plain.Err(); err != nil {
		t.Fatalf("Err(): %v", err)
	}

	cx, cy, r := testcard.CircleX, testcard.CircleY, testcard.CircleR
	var escaped []image.Point
	for y := range 300 {
		for x := range 400 {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= r*r {
				continue // inside the disc; fair game
			}
			if withCircle.ColorIndexAt(x, y) != plain.Image().ColorIndexAt(x, y) {
				escaped = append(escaped, image.Pt(x, y))
			}
		}
	}
	if len(escaped) > 0 {
		t.Errorf("%d pixels drawn outside the circle; first at %v, last at %v",
			len(escaped), escaped[0], escaped[len(escaped)-1])
	}

	// Guard against the test going vacuous. If the circle ever stopped
	// drawing anything, "nothing escaped" would be trivially true.
	drawn := 0
	for y := range 300 {
		for x := range 400 {
			if withCircle.ColorIndexAt(x, y) != plain.Image().ColorIndexAt(x, y) {
				drawn++
			}
		}
	}
	if drawn < 5000 {
		t.Errorf("the circle changed only %d pixels; this test is not exercising anything", drawn)
	}
}

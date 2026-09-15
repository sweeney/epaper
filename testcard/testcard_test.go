package testcard

import (
	"context"
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/internal/golden"
	"github.com/sweeney/epaper/mock"
	"github.com/sweeney/epaper/render"
	"golang.org/x/image/font/gofont/goregular"
)

var fourInk = epaper.Palette{
	{Ink: epaper.Black, RGB: color.RGBA{0, 0, 0, 255}},
	{Ink: epaper.White, RGB: color.RGBA{255, 255, 255, 255}},
	{Ink: epaper.Yellow, RGB: color.RGBA{235, 205, 40, 255}},
	{Ink: epaper.Red, RGB: color.RGBA{190, 45, 40, 255}},
}

// fixed lines, so goldens do not change with the clock.
var testLines = []string{"Red/Yellow wHAT (JD79668)", "400x300 4-ink"}

func draw(t *testing.T, opts Options) *render.Canvas {
	t.Helper()
	c := render.NewCanvas(image.Rect(0, 0, 400, 300), fourInk)
	Draw(c, opts)
	if err := c.Err(); err != nil {
		t.Fatalf("Err(): %v", err)
	}
	return c
}

// The card renders to a golden PNG entirely on a laptop, with no hardware
// anywhere near it.
func TestGoldenTestCard(t *testing.T) {
	golden.Assert(t, "testcard", draw(t, Options{Lines: testLines}).Image())
}

// The zero Options must work and must need no font file. This is the call a
// consumer makes first, and it should not require them to find a TTF.
func TestZeroOptionsNeedsNoFontFile(t *testing.T) {
	c := draw(t, Options{})
	if inked := countIndex(c, 3); inked == 0 {
		t.Error("the default card drew no red at all; is the title missing?")
	}
	if len(inksUsed(c)) != 4 {
		t.Errorf("the default card used %d inks, want 4", len(inksUsed(c)))
	}
}

// Caller lines are what identify a run, so they must actually reach the card.
func TestLinesAreDrawn(t *testing.T) {
	withLines := draw(t, Options{Lines: testLines}).Image()
	without := draw(t, Options{}).Image()

	diff := 0
	for i := range withLines.Pix {
		if withLines.Pix[i] != without.Pix[i] {
			diff++
		}
	}
	if diff == 0 {
		t.Error("Lines changed nothing on the card")
	}
}

// A line longer than the circle is wrapped, not shrunk and not dropped. The
// panel model off an EEPROM is routinely too long, which is why this exists.
func TestLongLinesWrapRatherThanVanish(t *testing.T) {
	long := draw(t, Options{Lines: []string{"Red/Yellow wHAT (JD79668) extremely long indeed"}})
	if err := long.Err(); err != nil {
		t.Fatalf("Err(): %v — a long line must wrap, not fail", err)
	}
	if countIndex(long, 0) == 0 {
		t.Error("nothing was drawn in black")
	}
}

// Many lines must not push content out of the circle or panic.
func TestTooManyLinesStayInside(t *testing.T) {
	var lines []string
	for range 20 {
		lines = append(lines, "overflowing line of text")
	}
	c := draw(t, Options{Lines: lines})
	assertNothingEscapesTheCircle(t, c.Image())
}

// Fonts are optional. Every diagnostic element still works without them;
// only the words are lost.
func TestNoText(t *testing.T) {
	c := draw(t, Options{NoText: true, Lines: testLines})
	if used := inksUsed(c); len(used) != 4 {
		t.Errorf("used %d inks with NoText, want all 4: %v", len(used), used)
	}
}

func TestFontsWith(t *testing.T) {
	fonts, err := FontsWith(goregular.TTF)
	if err != nil {
		t.Fatalf("FontsWith(): %v", err)
	}
	// Below the ceiling it must still hand back the bitmap faces — that is
	// the point, and overriding it makes small text worse, not different.
	small, err := fonts(10)
	if err != nil {
		t.Fatalf("fonts(10): %v", err)
	}
	bitmap, _ := Fonts()(10)
	if small != bitmap {
		t.Error("FontsWith returned a scaled outline below the bitmap ceiling")
	}
	// Above it, a scalable face.
	big, err := fonts(24)
	if err != nil {
		t.Fatalf("fonts(24): %v", err)
	}
	if render.LineHeight(big) <= render.LineHeight(small) {
		t.Error("the 24px face is not larger than the 10px one")
	}
}

func TestFontsWithRejectsRubbish(t *testing.T) {
	if _, err := FontsWith([]byte("not a font")); err == nil {
		t.Error("FontsWith() = nil error on invalid TrueType data")
	}
}

// The whole point of the card is that it exercises everything. If a change
// left one ink unused, the card would stop proving the panel can show it.
func TestUsesAllFourInks(t *testing.T) {
	c := draw(t, Options{Lines: testLines})
	for idx, name := range map[uint8]string{0: "black", 1: "white", 2: "yellow", 3: "red"} {
		if countIndex(c, idx) == 0 {
			t.Errorf("the card uses no %s at all", name)
		}
	}
}

// It must pack to exactly what the panel expects, or it is not an acceptance
// test for anything.
func TestCardPacks(t *testing.T) {
	got, err := epaper.Pack(draw(t, Options{Lines: testLines}).Image())
	if err != nil {
		t.Fatalf("Pack(): %v", err)
	}
	if len(got) != 30000 {
		t.Errorf("packed to %d bytes, want 30000", len(got))
	}
}

// Show is the documented one-liner, so it is worth testing that it works
// against the interface rather than only against a real panel.
func TestShow(t *testing.T) {
	dev := mock.New(400, 300, fourInk)
	defer dev.Close()

	if err := Show(context.Background(), dev, "a note"); err != nil {
		t.Fatalf("Show(): %v", err)
	}
	if len(dev.Frames()) != 1 {
		t.Fatalf("Frames() = %d, want 1", len(dev.Frames()))
	}
	assertNothingEscapesTheCircle(t, dev.Last())
}

func TestShowReportsDeviceFailures(t *testing.T) {
	dev := mock.New(400, 300, fourInk)
	defer dev.Close()
	dev.FailNextShow(context.DeadlineExceeded)

	if err := Show(context.Background(), dev); err == nil {
		t.Error("Show() = nil, want the device's error")
	}
}

// The conformance pattern is text-free, which is what makes it byte-comparable
// against the vendor library. A stray glyph would silently break that.
func TestConformanceUsesAllFourInks(t *testing.T) {
	c := render.NewCanvas(image.Rect(0, 0, 400, 300), fourInk)
	DrawConformance(c)
	if err := c.Err(); err != nil {
		t.Fatalf("Err(): %v", err)
	}
	if used := inksUsed(c); len(used) != 4 {
		t.Errorf("the conformance pattern used %d inks, want 4", len(used))
	}
}

// Nothing drawn for the circle may escape it.
//
// The circle is the card's one free-form region and everything in it is
// positioned from the circle's own geometry. Eyeballing a golden is how an
// overflow gets spotted; this is how it stays spotted.
func assertNothingEscapesTheCircle(t *testing.T, got *image.Paletted) {
	t.Helper()

	// The card's background is a 50% dither, so "escaped" cannot just mean
	// "a black pixel outside the circle". Compare against the same card drawn
	// without the circle: any pixel outside the disc the two disagree about
	// was put there by the circle.
	plain := render.NewCanvas(image.Rect(0, 0, 400, 300), fourInk)
	drawWithoutCircle(plain)
	if err := plain.Err(); err != nil {
		t.Fatalf("Err(): %v", err)
	}

	var escaped []image.Point
	changed := 0
	for y := range 300 {
		for x := range 400 {
			if got.ColorIndexAt(x, y) == plain.Image().ColorIndexAt(x, y) {
				continue
			}
			changed++
			dx, dy := x-circleX, y-circleY
			if dx*dx+dy*dy > circleR*circleR {
				escaped = append(escaped, image.Pt(x, y))
			}
		}
	}
	if len(escaped) > 0 {
		t.Errorf("%d pixels drawn outside the circle; first at %v, last at %v",
			len(escaped), escaped[0], escaped[len(escaped)-1])
	}
	// Guard against the check going vacuous: if the circle ever stopped
	// drawing anything, "nothing escaped" would be trivially true.
	if changed < 5000 {
		t.Errorf("the circle changed only %d pixels; this check is not exercising anything", changed)
	}
}

func TestCircleContentStaysInsideTheCircle(t *testing.T) {
	assertNothingEscapesTheCircle(t, draw(t, Options{Lines: testLines}).Image())
}

// wrapToCircle is the part of the layout most likely to go wrong quietly.
func TestWrapToCircle(t *testing.T) {
	f, _ := Fonts()(13)
	lines := wrapToCircle("Red/Yellow wHAT (JD79668)", f, circleY-40, render.LineHeight(f))
	if len(lines) < 2 {
		t.Errorf("wrapToCircle() = %q, want it split over more than one line", lines)
	}
	if joined := strings.Join(lines, " "); joined != "Red/Yellow wHAT (JD79668)" {
		t.Errorf("wrapping lost or reordered words: %q", joined)
	}
	if got := wrapToCircle("", f, circleY, 13); got != nil {
		t.Errorf("wrapToCircle(\"\") = %q, want nil", got)
	}
}

func countIndex(c *render.Canvas, idx uint8) int {
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

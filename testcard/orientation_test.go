package testcard

import (
	"image"
	"testing"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/render"
)

func orientation(t *testing.T, w, h int) *render.Canvas {
	t.Helper()
	c := render.NewCanvas(image.Rect(0, 0, w, h), fourInk)
	DrawOrientation(c)
	if err := c.Err(); err != nil {
		t.Fatalf("Err(): %v", err)
	}
	return c
}

// The goldens for this card live in panels_test.go, generated once per entry
// in inky.SupportedPanels() alongside the test card and the conformance
// pattern. A second copy here would be one more thing to regenerate and one
// more chance for the two to disagree.
//
// What is left in this file is the part a golden cannot assert: that the card
// is actually asymmetric, and therefore able to diagnose the thing it exists
// for.

// The card's whole job is to be different from every rotation and reflection
// of itself. If it is not, it cannot diagnose the thing it exists for.
//
// This is the test that would have caught a symmetric design, which is an easy
// mistake to make and an impossible one to notice by looking.
func TestOrientationCardIsAsymmetric(t *testing.T) {
	const w, h = 250, 122
	img := orientation(t, w, h).Image()

	at := func(x, y int) uint8 { return img.ColorIndexAt(x, y) }

	// A horizontal mirror and a vertical mirror must each change something.
	// (A quarter turn cannot be tested this way on a non-square panel — the
	// driver would reject the geometry long before it reached the glass.)
	for _, tc := range []struct {
		name string
		flip func(x, y int) uint8
	}{
		{"mirrored left-to-right", func(x, y int) uint8 { return at(w-1-x, y) }},
		{"mirrored top-to-bottom", func(x, y int) uint8 { return at(x, h-1-y) }},
		{"rotated 180", func(x, y int) uint8 { return at(w-1-x, h-1-y) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for y := range h {
				for x := range w {
					if at(x, y) != tc.flip(x, y) {
						return // different somewhere: good
					}
				}
			}
			t.Errorf("the card is identical to itself %s; it cannot diagnose an orientation fault", tc.name)
		})
	}
}

// Every ink must appear, or a missing one would read as a palette fault rather
// than as a card that never asked for it.
func TestOrientationCardUsesEveryInk(t *testing.T) {
	for _, size := range [][2]int{{250, 122}, {400, 300}, {64, 64}} {
		c := orientation(t, size[0], size[1])
		if got := len(inksUsed(c)); got != 4 {
			t.Errorf("%dx%d used %d inks, want 4", size[0], size[1], got)
		}
	}
}

// It must work on any panel, including absurd ones, without erroring or
// drawing outside its bounds. A card that panics on a small canvas is useless
// exactly when a new panel is being brought up.
func TestOrientationCardSurvivesAnyGeometry(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {8, 3}, {250, 122}, {122, 250}, {400, 300}, {1600, 1200}} {
		t.Run("", func(t *testing.T) {
			c := render.NewCanvas(image.Rect(0, 0, size[0], size[1]), fourInk)
			DrawOrientation(c)
			if err := c.Err(); err != nil {
				t.Errorf("%dx%d: %v", size[0], size[1], err)
			}
		})
	}
}

// The corners are the diagnosis, so pin what is in them. Read against the
// doc comment on DrawOrientation: if these disagree, the documentation is
// lying to whoever is squinting at a panel.
func TestOrientationCornersCarryTheDocumentedInks(t *testing.T) {
	const w, h = 250, 122
	img := orientation(t, w, h).Image()
	red, _ := fourInk.Index(epaper.Red)
	yellow, _ := fourInk.Index(epaper.Yellow)
	black, _ := fourInk.Index(epaper.Black)

	for _, tc := range []struct {
		name string
		x, y int
		want uint8
	}{
		{"top-left is red", 8, 8, red},
		{"top-right is yellow", w - 4, 8, yellow},
		{"bottom-left is black", 5, h - 3, black},
		{"top edge rule is black", w / 2, 1, black},
		{"left edge rule is red", 1, h / 2, red},
	} {
		if got := img.ColorIndexAt(tc.x, tc.y); got != tc.want {
			t.Errorf("%s: (%d,%d) is index %d, want %d", tc.name, tc.x, tc.y, got, tc.want)
		}
	}
}

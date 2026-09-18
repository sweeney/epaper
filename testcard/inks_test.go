package testcard

import (
	"fmt"
	"image"
	"testing"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/render"
)

func drawInks(t *testing.T, w, h int, p epaper.Palette) *render.Canvas {
	t.Helper()
	c := render.NewCanvas(image.Rect(0, 0, w, h), p)
	DrawInks(c)
	if err := c.Err(); err != nil {
		t.Fatalf("DrawInks at %dx%d: %v", w, h, err)
	}
	return c
}

// The diagonal of the matrix is each ink on its own, solid. This is the part
// of the card that settles "is blue blue": a driver that swapped two wire
// values draws a perfectly tidy card with two swatches in the wrong place,
// and only the labels beside them say so.
func TestInksCardHasASolidSwatchForEveryInk(t *testing.T) {
	for _, tc := range []struct {
		name string
		w, h int
		p    epaper.Palette
	}{
		{"six inks, 600x400", 600, 400, sixInk},
		{"four inks, 400x300", 400, 300, fourInk},
		{"four inks, 250x122", 250, 122, fourInk},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := drawInks(t, tc.w, tc.h, tc.p)
			g := newInkGrid(tc.w, tc.h, len(tc.p), Fonts())
			for i := range tc.p {
				cell := g.cell(i, i)
				if cell.Dx() < 4 || cell.Dy() < 4 {
					t.Fatalf("cell %d is %v, too small to be a swatch", i, cell)
				}
				for y := cell.Min.Y; y < cell.Max.Y; y++ {
					for x := cell.Min.X; x < cell.Max.X; x++ {
						if got := c.Image().ColorIndexAt(x, y); got != uint8(i) {
							t.Fatalf("%s swatch has index %d at (%d,%d)", tc.p[i].Ink, got, x, y)
						}
					}
				}
			}
		})
	}
}

// Off the diagonal, every pair of inks is mixed: a 1px checker above the
// diagonal, a 50% ordered dither below. Both are made of exactly the two inks
// of their row and column and nothing else, in roughly equal measure.
func TestInksCardMixesEveryPair(t *testing.T) {
	c := drawInks(t, 600, 400, sixInk)
	g := newInkGrid(600, 400, len(sixInk), Fonts())
	for row := range sixInk {
		for col := range sixInk {
			if row == col {
				continue
			}
			cell := g.cell(row, col)
			counts := map[uint8]int{}
			for y := cell.Min.Y; y < cell.Max.Y; y++ {
				for x := cell.Min.X; x < cell.Max.X; x++ {
					counts[c.Image().ColorIndexAt(x, y)]++
				}
			}
			a, b := counts[uint8(row)], counts[uint8(col)]
			if len(counts) != 2 || a == 0 || b == 0 {
				t.Errorf("cell (%s, %s) holds %v, want only those two inks", sixInk[row].Ink, sixInk[col].Ink, counts)
				continue
			}
			if diff := a - b; diff*diff*16 > (a+b)*(a+b) {
				t.Errorf("cell (%s, %s) is %d:%d, want about half and half", sixInk[row].Ink, sixInk[col].Ink, a, b)
			}
		}
	}
}

// Whatever the geometry — including sizes no panel has, since -size allows
// them — the card draws without failing, and the grid stays on the panel.
func TestInksCardSurvivesAnyGeometry(t *testing.T) {
	for _, a := range aspects {
		for _, p := range []epaper.Palette{fourInk, sixInk} {
			t.Run(fmt.Sprintf("%s-%d", a.name, len(p)), func(t *testing.T) {
				drawInks(t, a.w, a.h, p)
				g := newInkGrid(a.w, a.h, len(p), Fonts())
				last := g.cell(len(p)-1, len(p)-1)
				if !last.In(image.Rect(0, 0, a.w, a.h)) {
					t.Errorf("the last cell %v is off a %dx%d panel", last, a.w, a.h)
				}
			})
		}
	}
	for _, size := range []image.Point{{1, 1}, {7, 5}, {30, 30}} {
		drawInks(t, size.X, size.Y, sixInk)
	}
}

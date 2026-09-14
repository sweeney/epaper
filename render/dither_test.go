package render_test

import (
	"image"
	"testing"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/render"
)

func TestDitherExtremes(t *testing.T) {
	for _, tc := range []struct {
		ratio float64
		want  uint8
		why   string
	}{
		{0.0, white, "ratio 0 is all background"},
		{1.0, black, "ratio 1 is all ink"},
		{-0.5, white, "a negative ratio is all background"},
		{1.5, black, "a ratio above 1 is all ink"},
	} {
		c := newCanvas(8, 8)
		c.Fill(epaper.Yellow)
		c.Dither(image.Rect(0, 0, 8, 8), epaper.Black, epaper.White, tc.ratio)
		if err := c.Err(); err != nil {
			t.Fatalf("Err() = %v", err)
		}
		for y := range 8 {
			for x := range 8 {
				if got := at(t, c, x, y); got != tc.want {
					t.Fatalf("ratio %v: pixel (%d,%d) = %d, want %d (%s)", tc.ratio, x, y, got, tc.want, tc.why)
				}
			}
		}
	}
}

// The exact threshold rule, checked cell by cell against the matrix in
// testdata/README.md. Note the +0.5 and that the matrix is indexed [y][x];
// both matter, and getting either wrong still produces a plausible ramp.
func TestDitherMatchesTheBayerMatrix(t *testing.T) {
	bayer := [4][4]int{{0, 8, 2, 10}, {12, 4, 14, 6}, {3, 11, 1, 9}, {15, 7, 13, 5}}

	for _, ratio := range []float64{0.125, 0.25, 0.5, 0.75, 0.9} {
		c := newCanvas(4, 4)
		c.Dither(image.Rect(0, 0, 4, 4), epaper.Black, epaper.White, ratio)
		for y := range 4 {
			for x := range 4 {
				threshold := (float64(bayer[y][x]) + 0.5) / 16.0
				want := uint8(white)
				if ratio > threshold {
					want = black
				}
				if got := at(t, c, x, y); got != want {
					t.Errorf("ratio %v pixel (%d,%d): got %d, want %d (threshold %v)",
						ratio, x, y, got, want, threshold)
				}
			}
		}
	}
}

// The matrix is anchored to the canvas, not to the rectangle. Two adjacent
// dithered rectangles at the same ratio must tile seamlessly; anchoring to the
// rect would put a visible seam between them.
func TestDitherIsAnchoredToTheCanvas(t *testing.T) {
	whole := newCanvas(8, 4)
	whole.Dither(image.Rect(0, 0, 8, 4), epaper.Black, epaper.White, 0.5)

	split := newCanvas(8, 4)
	split.Dither(image.Rect(0, 0, 3, 4), epaper.Black, epaper.White, 0.5)
	split.Dither(image.Rect(3, 0, 8, 4), epaper.Black, epaper.White, 0.5)

	for y := range 4 {
		for x := range 8 {
			if a, b := at(t, whole, x, y), at(t, split, x, y); a != b {
				t.Fatalf("pixel (%d,%d): whole = %d, split = %d — the matrix moved with the rectangle", x, y, a, b)
			}
		}
	}
}

func TestDitherReportsAnAbsentInk(t *testing.T) {
	for _, tc := range []struct{ ink, bg epaper.Ink }{
		{epaper.Green, epaper.White},
		{epaper.Black, epaper.Blue},
	} {
		c := newCanvas(4, 4)
		c.Dither(image.Rect(0, 0, 4, 4), tc.ink, tc.bg, 0.5)
		if c.Err() == nil {
			t.Errorf("Dither(%s on %s): Err() = nil, want an error", tc.ink, tc.bg)
		}
	}
}

func TestChecker(t *testing.T) {
	c := newCanvas(4, 4)
	c.Checker(image.Rect(0, 0, 4, 4), epaper.Black, epaper.White, 1)
	for y := range 4 {
		for x := range 4 {
			want := uint8(black)
			if (x+y)%2 != 0 {
				want = white
			}
			if got := at(t, c, x, y); got != want {
				t.Errorf("pixel (%d,%d) = %d, want %d", x, y, got, want)
			}
		}
	}
}

func TestCheckerCellSize(t *testing.T) {
	c := newCanvas(8, 8)
	c.Checker(image.Rect(0, 0, 8, 8), epaper.Black, epaper.White, 2)
	// The top-left 2x2 block is all one colour.
	for _, p := range []image.Point{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
		if got := at(t, c, p.X, p.Y); got != black {
			t.Errorf("pixel (%d,%d) = %d, want black (inside the first cell)", p.X, p.Y, got)
		}
	}
	if got := at(t, c, 2, 0); got != white {
		t.Errorf("pixel (2,0) = %d, want white (the next cell along)", got)
	}
}

func TestCheckerIsAnchoredToTheCanvas(t *testing.T) {
	whole := newCanvas(8, 4)
	whole.Checker(image.Rect(0, 0, 8, 4), epaper.Black, epaper.White, 2)

	split := newCanvas(8, 4)
	split.Checker(image.Rect(0, 0, 3, 4), epaper.Black, epaper.White, 2)
	split.Checker(image.Rect(3, 0, 8, 4), epaper.Black, epaper.White, 2)

	for y := range 4 {
		for x := range 8 {
			if a, b := at(t, whole, x, y), at(t, split, x, y); a != b {
				t.Fatalf("pixel (%d,%d): whole = %d, split = %d — the grid moved with the rectangle", x, y, a, b)
			}
		}
	}
}

func TestCheckerRejectsABadCellSize(t *testing.T) {
	for _, cell := range []int{0, -1} {
		c := newCanvas(4, 4)
		c.Checker(image.Rect(0, 0, 4, 4), epaper.Black, epaper.White, cell)
		if c.Err() == nil {
			t.Errorf("Checker(cell=%d): Err() = nil, want an error", cell)
		}
	}
}

// Canvas coordinates can be negative when a sub-image is drawn into. Go's /
// and % truncate towards zero, which would mirror both patterns at the origin
// — the cell at x=-1 would repeat the one at x=0 instead of continuing.
func TestPatternsAreRegularAcrossTheOrigin(t *testing.T) {
	c := render.NewCanvas(image.Rect(-4, -4, 4, 4), fourInk)

	c.Checker(c.Bounds(), epaper.Black, epaper.White, 2)
	for y := -4; y < 4; y++ {
		for x := -4; x < 4; x++ {
			want := uint8(white)
			if (floorDiv(x, 2)+floorDiv(y, 2))%2 == 0 {
				want = black
			}
			if got := c.Image().ColorIndexAt(x, y); got != want {
				t.Fatalf("checker pixel (%d,%d) = %d, want %d", x, y, got, want)
			}
		}
	}

	c.Dither(c.Bounds(), epaper.Black, epaper.White, 0.5)
	bayer := [4][4]int{{0, 8, 2, 10}, {12, 4, 14, 6}, {3, 11, 1, 9}, {15, 7, 13, 5}}
	for y := -4; y < 4; y++ {
		for x := -4; x < 4; x++ {
			want := uint8(white)
			if 0.5 > (float64(bayer[((y%4)+4)%4][((x%4)+4)%4])+0.5)/16.0 {
				want = black
			}
			if got := c.Image().ColorIndexAt(x, y); got != want {
				t.Fatalf("dither pixel (%d,%d) = %d, want %d", x, y, got, want)
			}
		}
	}
}

// floorDiv mirrors the rule the implementation uses; see the test above.
func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

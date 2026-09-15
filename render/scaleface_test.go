package render_test

import (
	"image"
	"testing"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/render"
	"golang.org/x/image/font/basicfont"
)

func TestScaleFaceDegenerate(t *testing.T) {
	f := basicfont.Face7x13
	for _, n := range []int{0, 1, -3} {
		if got := render.ScaleFace(f, n); got != f {
			t.Errorf("ScaleFace(f, %d) returned a wrapper, want f unchanged", n)
		}
	}
	if render.ScaleFace(nil, 4) != nil {
		t.Error("ScaleFace(nil, 4) should be nil")
	}
}

// Scaling by n must multiply every metric by n, or layout code that measures
// text will place it wrongly.
func TestScaleFaceMetrics(t *testing.T) {
	base := basicfont.Face7x13
	for _, n := range []int{2, 3, 5} {
		scaled := render.ScaleFace(base, n)

		if got, want := render.MeasureText("Hello", scaled), render.MeasureText("Hello", base)*n; got != want {
			t.Errorf("n=%d: MeasureText = %d, want %d", n, got, want)
		}
		if got, want := render.LineHeight(scaled), render.LineHeight(base)*n; got != want {
			t.Errorf("n=%d: LineHeight = %d, want %d", n, got, want)
		}
	}
}

// The real property: a scaled glyph is the original with each pixel replaced
// by an n×n block, exactly. Anything else and the crispness is lost, which is
// the entire reason to prefer this over a scaled outline.
func TestScaleFaceIsExactPixelReplication(t *testing.T) {
	const n = 3
	base := basicfont.Face7x13

	small := render.NewCanvas(image.Rect(0, 0, 120, 40), fourInk)
	small.Fill(epaper.White)
	small.Text(image.Pt(0, 0), "Ag1", base, epaper.Black)

	big := render.NewCanvas(image.Rect(0, 0, 120*n, 40*n), fourInk)
	big.Fill(epaper.White)
	big.Text(image.Pt(0, 0), "Ag1", render.ScaleFace(base, n), epaper.Black)

	if err := small.Err(); err != nil {
		t.Fatalf("Err(): %v", err)
	}
	if err := big.Err(); err != nil {
		t.Fatalf("Err(): %v", err)
	}

	mismatches := 0
	for y := range 40 {
		for x := range 120 {
			want := small.Image().ColorIndexAt(x, y)
			for dy := range n {
				for dx := range n {
					if got := big.Image().ColorIndexAt(x*n+dx, y*n+dy); got != want {
						if mismatches < 5 {
							t.Errorf("pixel (%d,%d) block offset (%d,%d): got %d, want %d",
								x, y, dx, dy, got, want)
						}
						mismatches++
					}
				}
			}
		}
	}
	if mismatches > 0 {
		t.Fatalf("%d of %d scaled pixels differ from exact replication", mismatches, 120*40*n*n)
	}
}

// It must actually draw something recognisable through the normal Text path.
func TestScaleFaceDraws(t *testing.T) {
	c := render.NewCanvas(image.Rect(0, 0, 300, 80), fourInk)
	c.Fill(epaper.White)
	c.Text(image.Pt(4, 4), "BIG", render.ScaleFace(basicfont.Face7x13, 4), epaper.Black)
	if err := c.Err(); err != nil {
		t.Fatalf("Err(): %v", err)
	}
	if countInk(c, black) == 0 {
		t.Fatal("scaled text drew nothing")
	}
	// Four times the size means roughly sixteen times the ink.
	plain := render.NewCanvas(image.Rect(0, 0, 300, 80), fourInk)
	plain.Fill(epaper.White)
	plain.Text(image.Pt(4, 4), "BIG", basicfont.Face7x13, epaper.Black)

	ratio := float64(countInk(c, black)) / float64(countInk(plain, black))
	if ratio < 14 || ratio > 18 {
		t.Errorf("scaled ink is %.1fx the original, want about 16x", ratio)
	}
}

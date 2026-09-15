package testcard

import (
	"image"
	"testing"

	"github.com/sweeney/epaper/render"
)

// The whole card, which is the most drawing this library does in one go. It
// has to be negligible next to a 20 s refresh even on the slowest Pi.
func BenchmarkDraw(b *testing.B) {
	opts := Options{Lines: []string{"Red/Yellow wHAT (JD79668)", "400x300 4-ink"}}
	b.ReportAllocs()
	for b.Loop() {
		c := render.NewCanvas(image.Rect(0, 0, 400, 300), fourInk)
		Draw(c, opts)
		if err := c.Err(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDrawConformance(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		c := render.NewCanvas(image.Rect(0, 0, 400, 300), fourInk)
		DrawConformance(c)
		if err := c.Err(); err != nil {
			b.Fatal(err)
		}
	}
}

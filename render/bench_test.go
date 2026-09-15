package render_test

import (
	"image"
	"testing"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/render"
)

func benchCanvas() *render.Canvas {
	return render.NewCanvas(image.Rect(0, 0, 400, 300), fourInk)
}

func BenchmarkFill(b *testing.B) {
	c := benchCanvas()
	b.ReportAllocs()
	for b.Loop() {
		c.Fill(epaper.White)
	}
}

// Dither touches every pixel and does a table lookup per pixel, so it is the
// most expensive primitive by a distance.
func BenchmarkDitherFullPanel(b *testing.B) {
	c := benchCanvas()
	r := c.Bounds()
	b.ReportAllocs()
	for b.Loop() {
		c.Dither(r, epaper.Black, epaper.White, 0.5)
	}
}

func BenchmarkCheckerFullPanel(b *testing.B) {
	c := benchCanvas()
	r := c.Bounds()
	b.ReportAllocs()
	for b.Loop() {
		c.Checker(r, epaper.Black, epaper.White, 2)
	}
}

func BenchmarkEllipse(b *testing.B) {
	c := benchCanvas()
	r := image.Rect(50, 50, 250, 250)
	b.ReportAllocs()
	for b.Loop() {
		c.Ellipse(r, epaper.Yellow)
	}
}

func BenchmarkLine(b *testing.B) {
	c := benchCanvas()
	b.ReportAllocs()
	for b.Loop() {
		c.Line(image.Pt(0, 0), image.Pt(399, 299), epaper.Black)
	}
}

func BenchmarkText(b *testing.B) {
	c := benchCanvas()
	f := face(&testing.T{}, 16)
	b.ReportAllocs()
	for b.Loop() {
		c.Text(image.Pt(4, 4), "Hamburgefonstiv 0123", f, epaper.Black)
	}
}

// TextFitted builds a face per size it tries, so it is the one text call whose
// cost is worth knowing before putting it in a loop.
func BenchmarkTextFitted(b *testing.B) {
	c := benchCanvas()
	b.ReportAllocs()
	for b.Loop() {
		c.TextFitted(image.Rect(4, 4, 396, 40), "RED/YELLOW", testFamily, epaper.Black)
	}
}

func BenchmarkMeasureText(b *testing.B) {
	f := face(&testing.T{}, 16)
	b.ReportAllocs()
	for b.Loop() {
		render.MeasureText("Hamburgefonstiv 0123", f)
	}
}

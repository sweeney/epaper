package render_test

import (
	"bytes"
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/render"
)

// conformance draws testdata/conformance's pattern using nothing but the
// public Canvas API.
//
// Every shape here follows a rule written down in testdata/README.md. Nothing
// is tuned to match the fixture — if this and the fixture disagree, one of the
// two is wrong and the byte offset says which band to look at.
func conformance(c *render.Canvas) {
	const w, h = 400, 300

	// The generator starts from a white buffer; a Go canvas starts at index 0,
	// which is black. Bands 2 and 6 only paint their marks, not their ground.
	c.Fill(epaper.White)

	// band 1 (y 0..39): flat quadrants of all four inks
	for i, ink := range []epaper.Ink{epaper.Black, epaper.White, epaper.Yellow, epaper.Red} {
		c.Rect(image.Rect(i*100, 0, i*100+100, 40), ink)
	}

	// band 2 (y 40..99): 1px horizontal rules at pitch 2,3,4,5
	for i, pitch := range []int{2, 3, 4, 5} {
		x0 := i * 100
		for y := 40; y < 100; y++ {
			if (y-40)%pitch == 0 {
				c.Rect(image.Rect(x0, y, x0+100, y+1), epaper.Black)
			}
		}
	}

	// band 3 (y 100..159): checkerboards at cell 1, 2, 4, plus yellow/red
	for i, ck := range []struct {
		cell int
		a, b epaper.Ink
	}{
		{1, epaper.Black, epaper.White},
		{2, epaper.Black, epaper.White},
		{4, epaper.Black, epaper.White},
		{1, epaper.Yellow, epaper.Red},
	} {
		x0 := i * 100
		c.Checker(image.Rect(x0, 100, x0+100, 160), ck.a, ck.b, ck.cell)
	}

	// bands 4 and 5 (y 160..259): Bayer ramps, ratio 0.0 -> 1.0 left to right
	for _, band := range []struct {
		y0, y1  int
		ink, bg epaper.Ink
	}{
		{160, 220, epaper.Black, epaper.White},
		{220, 240, epaper.Yellow, epaper.White},
		{240, 260, epaper.Red, epaper.Black},
	} {
		for x := range w {
			ratio := float64(x) / float64(w-1)
			c.Dither(image.Rect(x, band.y0, x+1, band.y1), band.ink, band.bg, ratio)
		}
	}

	// band 6 (y 260..299): geometry primitives. Rectangles are half-open, so
	// the fixture's inclusive (4,264)-(96,295) is Rect(4,264,97,296).
	c.Rect(image.Rect(4, 264, 97, 296), epaper.Red)
	c.StrokeRect(image.Rect(4, 264, 97, 296), epaper.Black)
	c.Ellipse(image.Rect(104, 264, 165, 296), epaper.Yellow)
	c.StrokeEllipse(image.Rect(104, 264, 165, 296), epaper.Black)
	c.Line(image.Pt(172, 295), image.Pt(260, 264), epaper.Black)
	c.Line(image.Pt(172, 264), image.Pt(260, 295), epaper.Red)
	c.Polygon([]image.Point{{272, 295}, {316, 264}, {360, 295}}, epaper.Black)

	// single-pixel corner markers: these catch off-by-one, row/column
	// transposition and flips, all of which otherwise produce an image that
	// looks broadly plausible.
	c.Set(396, 296, epaper.Red)
	c.Set(399, 299, epaper.Black)
	c.Set(396, 299, epaper.Yellow)
	c.Set(399, 296, epaper.White)

	_ = h
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "testdata", "conformance", name))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	return b
}

// Check 1 of 3: render. A mismatch here is a drawing bug, and the byte offset
// names the band. See testdata/README.md §2.
func TestConformanceRender(t *testing.T) {
	c := render.NewCanvas(image.Rect(0, 0, 400, 300), fourInk)
	conformance(c)
	if err := c.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}

	want := fixture(t, "conformance.idx")
	got := c.Image().Pix
	if len(got) != len(want) {
		t.Fatalf("rendered %d indices, want %d", len(got), len(want))
	}
	if bytes.Equal(got, want) {
		return
	}

	// Localise the failure: which band, how many pixels, and where first.
	const w = 400
	perBand := map[string]int{}
	first := -1
	for i := range want {
		if got[i] != want[i] {
			if first < 0 {
				first = i
			}
			perBand[bandName(i/w)]++
		}
	}
	t.Fatalf("rendered pattern does not match the fixture: %d of %d px differ\n"+
		"  first at (%d,%d): got index %d, want %d\n"+
		"  by band: %v",
		countAll(perBand), len(want), first%w, first/w, got[first], want[first], perBand)
}

// Check 3 of 3: end to end. Render in Go, pack, compare against the bytes the
// vendor library would have sent. Check 2 (packing in isolation) lives in the
// root package, so a failure here after that one passes is a drawing bug.
func TestConformanceEndToEnd(t *testing.T) {
	c := render.NewCanvas(image.Rect(0, 0, 400, 300), fourInk)
	conformance(c)
	if err := c.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}

	got, err := epaper.Pack(c.Image())
	if err != nil {
		t.Fatalf("Pack() error: %v", err)
	}
	want := fixture(t, "conformance.bin")
	if !bytes.Equal(got, want) {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("packed framebuffer differs at byte %d: got %08b, want %08b "+
					"(pixels %d..%d, row %d)", i, got[i], want[i], i*4, i*4+3, i*4/400)
			}
		}
		t.Fatalf("packed framebuffer length = %d, want %d", len(got), len(want))
	}
}

func bandName(y int) string {
	switch {
	case y < 40:
		return "1 flat fills"
	case y < 100:
		return "2 rules"
	case y < 160:
		return "3 checkers"
	case y < 220:
		return "4 bayer black"
	case y < 260:
		return "5 bayer accent"
	default:
		return "6 geometry"
	}
}

func countAll(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

package render_test

import (
	"bytes"
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/render"
	"github.com/sweeney/epaper/testcard"
)

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
	testcard.DrawConformance(c)
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
	testcard.DrawConformance(c)
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

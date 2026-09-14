package golden_test

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sweeney/epaper/internal/golden"
)

func solid(w, h int, c color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	return img
}

// Round-trip: writing a golden then asserting against it must pass. Uses a
// throwaway name so it never collides with a real fixture.
func TestAssertRoundTrip(t *testing.T) {
	const name = "internal-golden-selftest"
	path := filepath.Join(golden.Dir(t), name+".png")
	t.Cleanup(func() { os.Remove(path) })

	img := solid(8, 4, color.RGBA{10, 20, 30, 255})

	*golden.Update = true
	golden.Assert(t, name, img)
	*golden.Update = false

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("golden was not written: %v", err)
	}
	golden.Assert(t, name, img) // must not fail
}

// The comparison is the part worth testing directly; Assert is thin plumbing
// over it. A mismatch has to say how bad it is and where, because on a
// four-colour panel "which pixels moved" is the whole question.
func TestCompare(t *testing.T) {
	black := solid(8, 4, color.RGBA{0, 0, 0, 255})

	if got := golden.Compare(black, solid(8, 4, color.RGBA{0, 0, 0, 255})); got != "" {
		t.Errorf("Compare() on identical images = %q, want \"\"", got)
	}

	if got := golden.Compare(black, solid(16, 4, color.RGBA{0, 0, 0, 255})); got == "" {
		t.Error("Compare() on differing bounds = \"\", want a description")
	} else if !strings.Contains(got, "bounds") {
		t.Errorf("Compare() on differing bounds = %q, want it to mention bounds", got)
	}

	got := golden.Compare(solid(8, 4, color.RGBA{255, 255, 255, 255}), black)
	for _, want := range []string{"32 of 32", "100.00%", "(0,0)"} {
		if !strings.Contains(got, want) {
			t.Errorf("Compare() = %q, want it to contain %q", got, want)
		}
	}
}

// One differing pixel must be located precisely, not just counted.
func TestCompareLocatesASinglePixel(t *testing.T) {
	a := image.NewRGBA(image.Rect(0, 0, 8, 4))
	b := image.NewRGBA(image.Rect(0, 0, 8, 4))
	b.Set(5, 2, color.RGBA{255, 0, 0, 255})

	got := golden.Compare(a, b)
	if !strings.Contains(got, "(5,2)") {
		t.Errorf("Compare() = %q, want it to locate the pixel at (5,2)", got)
	}
	if !strings.Contains(got, "1 of 32") {
		t.Errorf("Compare() = %q, want it to report 1 of 32 pixels", got)
	}
}

func TestDirIsUnderTestdata(t *testing.T) {
	dir := golden.Dir(t)
	if base := filepath.Base(dir); base != "golden" {
		t.Errorf("Dir() = %s, want a path ending in testdata/golden", dir)
	}
	if parent := filepath.Base(filepath.Dir(dir)); parent != "testdata" {
		t.Errorf("Dir() = %s, want a path ending in testdata/golden", dir)
	}
	// It must be the module root's testdata, not the golden package's.
	if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(dir)), "go.mod")); err != nil {
		t.Errorf("Dir() = %s, which is not directly under the module root", dir)
	}
}

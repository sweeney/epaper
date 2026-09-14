// Package golden compares rendered images against reviewed reference PNGs.
//
// It is test-only infrastructure for things that cannot be checked against the
// vendor library — anything involving text, where Pillow and x/image/font
// rasterise glyphs differently and no byte-exact comparison is possible. For
// the parts that *can* be compared byte-exactly, use testdata/conformance
// instead; it is a stronger check.
//
// # Regenerating
//
//	go test ./render -update
//
// Then read the diff. A golden regenerated without being looked at asserts
// nothing at all — it records whatever the code does today, including the bug
// you were about to find. This is the whole risk of golden testing and the
// only defence is the habit.
package golden

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Update reports whether -update was passed, in which case [Assert] rewrites
// the golden files instead of comparing against them.
var Update = flag.Bool("update", false, "rewrite golden PNGs in testdata/golden instead of comparing against them")

// Assert compares img against testdata/golden/<name>.png.
//
// With -update it writes the file instead. Without, a mismatch fails the test
// and writes the actual output alongside the golden as <name>.got.png, so the
// two can be opened side by side — on a 400x300 four-colour panel, "which
// pixels moved" is a question best answered by looking.
func Assert(t *testing.T, name string, img image.Image) {
	t.Helper()

	path := filepath.Join(Dir(t), name+".png")
	if *Update {
		if err := write(path, img); err != nil {
			t.Fatalf("golden: writing %s: %v", path, err)
		}
		t.Logf("golden: wrote %s — review the diff before committing", path)
		return
	}

	want, err := read(path)
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("golden: %s does not exist; create it with:\n\tgo test ./... -update", path)
		}
		t.Fatalf("golden: reading %s: %v", path, err)
	}

	if diff := Compare(img, want); diff != "" {
		got := filepath.Join(Dir(t), name+".got.png")
		if err := write(got, img); err != nil {
			t.Logf("golden: could not write %s: %v", got, err)
		} else {
			t.Logf("golden: actual output written to %s", got)
		}
		t.Errorf("golden: %s does not match:\n\t%s", name+".png", diff)
	}
}

// Dir returns the testdata/golden directory, creating it if need be.
//
// It is located relative to the module root rather than the current package,
// so every package's goldens live together and a test can be moved between
// packages without its fixtures going missing.
func Dir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(moduleRoot(t), "testdata", "golden")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("golden: creating %s: %v", dir, err)
	}
	return dir
}

// moduleRoot walks up from this source file until it finds the go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("golden: cannot determine the path of this source file")
	}
	dir := filepath.Dir(self)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("golden: no go.mod found above %s", filepath.Dir(self))
		}
		dir = parent
	}
}

func write(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func read(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

// Compare returns a human-readable description of how two images differ, or
// "" if they match. It is exported because it, not the file plumbing around
// it, is the part worth testing directly.
//
// Comparison is on RGBA values rather than palette indices, because a PNG
// round-trip is free to renumber the palette.
func Compare(got, want image.Image) string {
	gb, wb := got.Bounds(), want.Bounds()
	if gb != wb {
		return fmt.Sprintf("bounds are %v, want %v", gb, wb)
	}

	var first image.Point
	n := 0
	var gotAt, wantAt color.Color
	for y := gb.Min.Y; y < gb.Max.Y; y++ {
		for x := gb.Min.X; x < gb.Max.X; x++ {
			g, w := got.At(x, y), want.At(x, y)
			gr, gg, gbl, ga := g.RGBA()
			wr, wg, wbl, wa := w.RGBA()
			if gr != wr || gg != wg || gbl != wbl || ga != wa {
				if n == 0 {
					first, gotAt, wantAt = image.Pt(x, y), g, w
				}
				n++
			}
		}
	}
	if n == 0 {
		return ""
	}
	total := gb.Dx() * gb.Dy()
	return fmt.Sprintf("%d of %d pixels differ (%.2f%%); first at (%d,%d): got %v, want %v",
		n, total, 100*float64(n)/float64(total), first.X, first.Y, gotAt, wantAt)
}

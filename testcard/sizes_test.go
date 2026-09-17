package testcard_test

import (
	"testing"

	"github.com/sweeney/epaper/render"
	"github.com/sweeney/epaper/testcard"
)

// From issue #4: "there is exactly one usable headline size on this panel",
// discovered by experiment because nothing reports the ladder.
//
// FontSizes does. A layout can then ask rather than guess, and a consumer
// cannot design toward a size that does not exist.
func TestFontSizes(t *testing.T) {
	got := testcard.FontSizes()
	want := []int{13, 17, 34, 51, 68}
	if len(got) != len(want) {
		t.Fatalf("FontSizes() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("FontSizes() = %v, want %v", got, want)
		}
	}
}

// The list has to be the truth, not a copy of it that can drift. Every size it
// names must come back at exactly that line height, and no size it omits may
// introduce a new one.
func TestFontSizesMatchesTheFamily(t *testing.T) {
	ff := testcard.Fonts()
	listed := map[int]bool{}
	for _, s := range testcard.FontSizes() {
		listed[s] = true
		f, err := ff(s)
		if err != nil {
			t.Fatalf("Fonts()(%d): %v", s, err)
		}
		if lh := render.LineHeight(f); lh != s {
			t.Errorf("FontSizes lists %d but Fonts()(%d) has line height %d", s, s, lh)
		}
	}

	// Sweep the whole usable range: every distinct line height the family can
	// produce must be one FontSizes named.
	for size := 1; size <= 200; size++ {
		f, err := ff(size)
		if err != nil {
			t.Fatalf("Fonts()(%d): %v", size, err)
		}
		lh := render.LineHeight(f)
		if !listed[lh] && lh <= testcard.FontSizes()[len(testcard.FontSizes())-1] {
			t.Errorf("Fonts()(%d) has line height %d, which FontSizes does not list", size, lh)
		}
	}
}

// The returned slice must be a copy. A caller sorting or truncating it in
// place would otherwise corrupt it for everyone else in the process.
func TestFontSizesIsACopy(t *testing.T) {
	a := testcard.FontSizes()
	a[0] = 999
	if b := testcard.FontSizes(); b[0] == 999 {
		t.Error("FontSizes returns its backing array; a caller can corrupt it")
	}
}

// LargestFontSizeFor is the question a layout actually asks: what is the
// biggest face that fits this box?
func TestLargestFontSizeFor(t *testing.T) {
	for _, tc := range []struct {
		height int
		want   int
	}{
		{200, 68}, {68, 68}, {67, 51}, {51, 51}, {50, 34},
		{34, 34}, {33, 17}, {17, 17}, {16, 13}, {13, 13},
		{12, 0}, {0, 0}, {-1, 0},
	} {
		if got := testcard.LargestFontSizeFor(tc.height); got != tc.want {
			t.Errorf("LargestFontSizeFor(%d) = %d, want %d", tc.height, got, tc.want)
		}
	}
}

// And what it reports must actually fit, at every height, or it is worse than
// nothing.
func TestLargestFontSizeForAlwaysFits(t *testing.T) {
	ff := testcard.Fonts()
	for h := 1; h <= 120; h++ {
		size := testcard.LargestFontSizeFor(h)
		if size == 0 {
			continue
		}
		f, err := ff(size)
		if err != nil {
			t.Fatalf("Fonts()(%d): %v", size, err)
		}
		if lh := render.LineHeight(f); lh > h {
			t.Errorf("LargestFontSizeFor(%d) = %d, whose line height is %d", h, size, lh)
		}
	}
}

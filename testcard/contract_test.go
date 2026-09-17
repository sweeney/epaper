package testcard_test

import (
	"testing"

	"github.com/sweeney/epaper/render"
	"github.com/sweeney/epaper/testcard"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
)

// The round-down contract, promoted from a sentence in Fonts' doc comment to
// something render.FontFamily requires of every implementation.
//
// It is load-bearing in a way that was not obvious until a consumer leant on
// it (issue #4). Asking a family for a box's height and trusting the result to
// fit is the family-AGNOSTIC way to size text: it needs no knowledge of any
// particular ladder, so a screen package can take a FontFamily as a parameter
// and still lay out correctly. That only works if rounding down is a promise
// rather than a happy accident of the bundled family.
//
// FontsWith did not honour it. An outline face at Size=N has a line height
// around 1.17*N, so every size it returned was too tall — by up to 13px, which
// is a whole line of body text.
func TestFamiliesNeverReturnATallerFaceThanAsked(t *testing.T) {
	withTTF, err := testcard.FontsWith(goregular.TTF)
	if err != nil {
		t.Fatalf("FontsWith(): %v", err)
	}

	for _, fam := range []struct {
		name string
		ff   render.FontFamily
	}{
		{"Fonts()", testcard.Fonts()},
		{"FontsWith(goregular)", withTTF},
	} {
		t.Run(fam.name, func(t *testing.T) {
			// From 13 up: below that no bundled face exists, which is the
			// documented exception and is checked separately.
			for size := 13; size <= 200; size++ {
				f, err := fam.ff(size)
				if err != nil {
					t.Fatalf("%s(%d): %v", fam.name, size, err)
				}
				if lh := render.LineHeight(f); lh > size {
					t.Fatalf("%s(%d) returned a face %dpx tall — %dpx too big",
						fam.name, size, lh, lh-size)
				}
			}
		})
	}
}

// The documented exception: below the smallest face there is nothing to give,
// so the family returns that face and the caller must notice. It must be the
// ONLY exception, and it must stop at 13.
func TestFamiliesFloorAtTheSmallestFace(t *testing.T) {
	ff := testcard.Fonts()
	for size := 1; size < 13; size++ {
		f, err := ff(size)
		if err != nil {
			t.Fatalf("Fonts()(%d): %v", size, err)
		}
		if lh := render.LineHeight(f); lh != 13 {
			t.Errorf("Fonts()(%d) has line height %d, want the 13px floor", size, lh)
		}
	}
}

// The consumer's pattern from issue #4, as a test: ask for the height you have
// and draw with what comes back. It must fit, for every family and every box
// down to the floor — this is the thing the contract exists to make safe.
func TestAskingForABoxHeightYieldsSomethingThatFits(t *testing.T) {
	withTTF, err := testcard.FontsWith(goregular.TTF)
	if err != nil {
		t.Fatalf("FontsWith(): %v", err)
	}
	for _, ff := range []render.FontFamily{testcard.Fonts(), withTTF} {
		for h := 13; h <= 120; h++ {
			f, err := ff(h)
			if err != nil {
				t.Fatalf("family(%d): %v", h, err)
			}
			if lh := render.LineHeight(f); lh > h {
				t.Fatalf("asked for a %dpx box and got a %dpx face", h, lh)
			}
		}
	}
}

// FontsWith must still actually scale — a fix that clamped everything to the
// bitmap face would satisfy the contract and be useless.
func TestFontsWithStillGrows(t *testing.T) {
	ff, err := testcard.FontsWith(goregular.TTF)
	if err != nil {
		t.Fatalf("FontsWith(): %v", err)
	}
	small, _ := ff(20)
	large, _ := ff(80)
	if render.LineHeight(large) <= render.LineHeight(small) {
		t.Fatalf("FontsWith(80) is %dpx and FontsWith(20) is %dpx; it stopped scaling",
			render.LineHeight(large), render.LineHeight(small))
	}
	// And it must get reasonably close to what was asked for, or a caller's
	// headline silently shrinks.
	if lh := render.LineHeight(large); lh < 80*3/4 {
		t.Errorf("FontsWith(80) is only %dpx; it is rounding down far too hard", lh)
	}
}

// sameFace reports whether two faces behave identically for layout purposes.
//
// Pointer equality will not do: Fonts() hands back a fresh closure with its own
// cache, so two calls produce equivalent-but-distinct ScaleFace values. What
// matters here is whether a caller would get the same rendering, which is the
// metrics.
func sameFace(a, b font.Face) bool {
	const probe = "CHEAPEST 02:00"
	return render.LineHeight(a) == render.LineHeight(b) &&
		render.MeasureText(probe, a) == render.MeasureText(probe, b) &&
		a.Metrics().CapHeight == b.Metrics().CapHeight
}

// From issue #4's second follow-up: FontsWith crossed over to an outline at
// 17px, and an outline built to fit a 17px LINE height has a cap height around
// 10px — which is the legibility floor this package's own bench found, and
// squarely inside the range its docs say an outline cannot serve.
//
// The round-down fix made it visible rather than causing it: before, a request
// for 17 returned something ~20px tall, so the glyphs were bigger and the
// problem was partly masked by a different bug.
func TestFontsWithKeepsBitmapsWhereOutlinesFail(t *testing.T) {
	ff, err := testcard.FontsWith(goregular.TTF)
	if err != nil {
		t.Fatalf("FontsWith(): %v", err)
	}
	bmp := testcard.Fonts()

	// Below the crossover the outline family must hand back the SAME face the
	// bitmap family would, not a small outline.
	for size := 13; size < testcard.DefaultOutlineFrom; size++ {
		got, err := ff(size)
		if err != nil {
			t.Fatalf("FontsWith(%d): %v", size, err)
		}
		want, err := bmp(size)
		if err != nil {
			t.Fatalf("Fonts(%d): %v", size, err)
		}
		if !sameFace(got, want) {
			t.Errorf("FontsWith(%d) returned an outline; below %d it must stay bitmap",
				size, testcard.DefaultOutlineFrom)
		}
	}

	// At and above it, the outline takes over and its glyphs are big enough to
	// survive thresholding on a panel with no intermediate tones.
	for _, size := range []int{testcard.DefaultOutlineFrom, 34, 51} {
		f, err := ff(size)
		if err != nil {
			t.Fatalf("FontsWith(%d): %v", size, err)
		}
		if bf, _ := bmp(size); sameFace(f, bf) {
			t.Errorf("FontsWith(%d) is still the bitmap face; the crossover is too high", size)
		}
		if cap := f.Metrics().CapHeight.Round(); cap < 15 {
			t.Errorf("FontsWith(%d) has a cap height of %dpx, below what an outline can serve", size, cap)
		}
	}
}

// The crossover is configurable, because the right value depends on the face.
func TestFontsWithFrom(t *testing.T) {
	bmp := testcard.Fonts()
	for _, from := range []int{20, 26, 40} {
		ff, err := testcard.FontsWithFrom(goregular.TTF, from)
		if err != nil {
			t.Fatalf("FontsWithFrom(%d): %v", from, err)
		}
		below, _ := ff(from - 1)
		if want, _ := bmp(from - 1); !sameFace(below, want) {
			t.Errorf("FontsWithFrom(%d) used an outline at %d", from, from-1)
		}
		at, _ := ff(from)
		if want, _ := bmp(from); sameFace(at, want) {
			t.Errorf("FontsWithFrom(%d) did not switch to the outline at %d", from, from)
		}
		// Round-down still holds either side of the crossover.
		for size := 13; size <= 80; size++ {
			f, err := ff(size)
			if err != nil {
				t.Fatalf("FontsWithFrom(%d)(%d): %v", from, size, err)
			}
			if lh := render.LineHeight(f); lh > size {
				t.Fatalf("FontsWithFrom(%d)(%d) is %dpx tall", from, size, lh)
			}
		}
	}
}

// A crossover below the smallest bitmap is meaningless and must be refused
// rather than silently producing 8px outlines.
func TestFontsWithFromRejectsATooLowCrossover(t *testing.T) {
	if _, err := testcard.FontsWithFrom(goregular.TTF, 12); err == nil {
		t.Error("FontsWithFrom(.., 12) = nil error; an outline cannot serve that size")
	}
	if _, err := testcard.FontsWithFrom([]byte("not a font"), 26); err == nil {
		t.Error("FontsWithFrom with rubbish TTF = nil error")
	}
}

// FontRungs carries the advance beside the size, so a caller sizing against
// WIDTH can see that going up a rung can widen text without rendering both and
// diffing the pixels — which is what the reporter had to do.
func TestFontRungs(t *testing.T) {
	rungs := testcard.FontRungs()
	sizes := testcard.FontSizes()
	if len(rungs) != len(sizes) {
		t.Fatalf("FontRungs has %d entries, FontSizes has %d", len(rungs), len(sizes))
	}
	ff := testcard.Fonts()
	for i, r := range rungs {
		if r.Size != sizes[i] {
			t.Errorf("rung %d is size %d, FontSizes says %d", i, r.Size, sizes[i])
		}
		f, _ := ff(r.Size)
		if got := render.MeasureText("M", f); got != r.Advance {
			t.Errorf("rung %d (%dpx) advertises advance %d, the face measures %d",
				i, r.Size, r.Advance, got)
		}
	}

	// The property the reporter was bitten by must be visible in this data
	// without drawing anything.
	var widened, narrowed int
	for i := 1; i < len(rungs); i++ {
		switch {
		case rungs[i].Advance > rungs[i-1].Advance:
			widened++
		case rungs[i].Advance < rungs[i-1].Advance:
			narrowed++
		}
	}
	if narrowed == 0 {
		t.Error("no rung is narrower than the one below it; the ladder's width " +
			"non-monotonicity is the thing this exists to expose")
	}
	t.Logf("across %d rungs: %d widen, %d NARROW going up", len(rungs), widened, narrowed)
}

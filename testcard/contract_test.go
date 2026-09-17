package testcard_test

import (
	"testing"

	"github.com/sweeney/epaper/render"
	"github.com/sweeney/epaper/testcard"
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

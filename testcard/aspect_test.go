package testcard

import (
	"fmt"
	"image"
	"testing"

	"github.com/sweeney/epaper/internal/golden"
	"github.com/sweeney/epaper/render"
)

// The card is laid out from the panel's bounds, and the derivation quietly
// assumed the panel is wider than it is tall — the disc's radius comes from
// the height while its centre comes from the width. That holds for every panel
// this library drives and fails for every portrait one.
//
// No portrait panel is supported today, so this is not reachable through
// inky.Open. It IS reachable through "epaper-testcard -size 122x250", which
// exists so an unsold geometry can be looked at before anyone writes a driver
// for it — and a diagnostic card that is itself clipped is worse than useless
// for exactly that job. At 122x250 the unclamped disc spanned x -4..126 on a
// 122px panel, cut off flat against both edges.
//
// These are the cheap half of "is portrait worth testing". The expensive half
// is presenting a panel in portrait at all, which is rotation — PLAN §9.9,
// still deferred.

// aspects covers both real panels, both of them rotated, and a square, so a
// layout rule that only holds for landscape cannot pass.
var aspects = []struct {
	name string
	w, h int
}{
	{"wHAT landscape", 400, 300},
	{"wHAT portrait", 300, 400},
	{"pHAT landscape", 250, 122},
	{"pHAT portrait", 122, 250},
	{"square", 200, 200},
	{"very wide", 400, 100},
	{"very tall", 100, 400},
}

// Every element must be inside the panel and clear of every other element.
// Overlap is not a lesser failure than clipping: a step wedge hidden under the
// disc measures nothing, and the card exists to measure.
func TestLayoutHoldsAtEveryAspectRatio(t *testing.T) {
	for _, a := range aspects {
		t.Run(a.name, func(t *testing.T) {
			l := newLayout(a.w, a.h)

			if l.circleX-l.circleR < 0 || l.circleX+l.circleR >= a.w {
				t.Errorf("disc spans x %d..%d on a %dpx-wide panel",
					l.circleX-l.circleR, l.circleX+l.circleR, a.w)
			}
			if l.circleY-l.circleR < 0 || l.circleY+l.circleR >= a.h {
				t.Errorf("disc spans y %d..%d on a %dpx-tall panel",
					l.circleY-l.circleR, l.circleY+l.circleR, a.h)
			}

			// The wedges are the innermost thing the disc could run into.
			if wedgeRight := l.wedgeInset + l.wedgeW; l.circleX-l.circleR <= wedgeRight {
				t.Errorf("disc reaches x %d but the left step wedge ends at %d; they overlap",
					l.circleX-l.circleR, wedgeRight)
			}
			// And the ladders are outside the wedges.
			if l.border+l.ladderBarW > l.wedgeInset {
				t.Errorf("ladder ends at x %d but the wedge starts at %d; they overlap",
					l.border+l.ladderBarW, l.wedgeInset)
			}
		})
	}
}

// ...and the card must actually draw at all of them, without the canvas
// recording a failure and turning the rest of the drawing into no-ops.
func TestCardDrawsAtEveryAspectRatio(t *testing.T) {
	for _, a := range aspects {
		t.Run(a.name, func(t *testing.T) {
			c := render.NewCanvas(image.Rect(0, 0, a.w, a.h), fourInk)
			Draw(c, Options{Lines: []string{"Red/Yellow pHAT (JD79661)", "a second line"}})
			if err := c.Err(); err != nil {
				t.Fatalf("Draw(): %v", err)
			}
			if got := len(inksUsed(c)); got != 4 {
				t.Errorf("used %d inks, want 4", got)
			}
		})
	}
}

// The clamps above must be no-ops on the panels that actually exist. This is
// what lets the reviewed 400x300 and 250x122 goldens stand as proof that
// nothing moved — without it, a future clamp could quietly reshape a real
// panel's card and only a regenerated golden would show it.
func TestLayoutIsUnchangedOnTheRealPanels(t *testing.T) {
	for _, tc := range []struct {
		w, h                      int
		border, castle            int
		circleX, circleY, circleR int
	}{
		// The literals the card was originally written with, before the
		// layout was derived. At 400x300 the arithmetic must be the identity.
		{400, 300, 12, 25, 200, 148, 78},
		// And the pHAT's, as reviewed on the panel on 2026-09-16.
		{250, 122, 4, 10, 125, 60, 31},
	} {
		t.Run(fmt.Sprintf("%dx%d", tc.w, tc.h), func(t *testing.T) {
			l := newLayout(tc.w, tc.h)
			for _, f := range []struct {
				name      string
				got, want int
			}{
				{"border", l.border, tc.border},
				{"castle", l.castle, tc.castle},
				{"circleX", l.circleX, tc.circleX},
				{"circleY", l.circleY, tc.circleY},
				{"circleR", l.circleR, tc.circleR},
			} {
				if f.got != f.want {
					t.Errorf("%s = %d, want %d — a real panel's layout moved", f.name, f.got, f.want)
				}
			}
		})
	}
}

// One portrait golden, so the aspect-ratio case is something a human looks at
// rather than only a set of inequalities. It is rendered into the CI report
// alongside the supported panels.
//
// 122x250 is the pHAT turned on its side: the nearest thing to a portrait
// panel this library could plausibly meet.
func TestGoldenPortrait(t *testing.T) {
	c := render.NewCanvas(image.Rect(0, 0, 122, 250), fourInk)
	Draw(c, Options{Lines: []string{"portrait 122x250"}})
	if err := c.Err(); err != nil {
		t.Fatalf("Draw(): %v", err)
	}
	golden.Assert(t, "aspect-122x250-portrait", c.Image())
}

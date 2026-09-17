package render_test

import (
	"image"
	"testing"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/render"
)

// From issue #5: Dither takes a float, so it reads like a knob you can turn.
// It is not — the 4x4 matrix quantises it to 17 outputs — and a caller typing
// numbers and looking at the result cannot tell "no change" from "too small a
// change". Same shape as the font ladder: a discrete set behind a continuous
// interface. Name the rungs.
func TestDitherLevels(t *testing.T) {
	got := render.DitherLevels()
	if len(got) != 17 {
		t.Fatalf("DitherLevels() has %d entries, want 17", len(got))
	}
	for k, ratio := range got {
		if want := float64(k) / 16; ratio != want {
			t.Errorf("level %d = %v, want %v", k, ratio, want)
		}
	}
	if got[0] != 0 || got[16] != 1 {
		t.Errorf("levels run %v..%v, want 0..1", got[0], got[16])
	}
}

// The promise that makes the list worth having: level k inks exactly k
// sixteenths of the area. Not approximately — exactly.
func TestDitherLevelsAreExact(t *testing.T) {
	const side = 64 // a multiple of 4, so the matrix tiles whole
	for k, ratio := range render.DitherLevels() {
		c := render.NewCanvas(image.Rect(0, 0, side, side), fourInk)
		c.Fill(epaper.White)
		c.Dither(c.Bounds(), epaper.Black, epaper.White, ratio)
		want := side * side * k / 16
		if got := countInk(c, 0); got != want {
			t.Errorf("level %d (ratio %v) inked %d of %d pixels, want exactly %d",
				k, ratio, got, side*side, want)
		}
	}
}

// Every level must differ from its neighbours, or the list is advertising
// choices that are not choices.
func TestDitherLevelsAreAllDistinct(t *testing.T) {
	seen := map[int]float64{}
	for _, ratio := range render.DitherLevels() {
		c := render.NewCanvas(image.Rect(0, 0, 64, 64), fourInk)
		c.Fill(epaper.White)
		c.Dither(c.Bounds(), epaper.Black, epaper.White, ratio)
		n := countInk(c, 0)
		if prev, dup := seen[n]; dup {
			t.Errorf("ratios %v and %v both ink %d pixels", prev, ratio, n)
		}
		seen[n] = ratio
	}
}

// And nothing between the levels does anything new — which is the claim that
// makes this a complete list rather than a helpful subset. 1000 steps across
// the range must produce exactly the 17 outputs the levels do.
func TestNoRatioProducesAnythingOffTheLadder(t *testing.T) {
	ladder := map[int]bool{}
	for _, ratio := range render.DitherLevels() {
		c := render.NewCanvas(image.Rect(0, 0, 64, 64), fourInk)
		c.Fill(epaper.White)
		c.Dither(c.Bounds(), epaper.Black, epaper.White, ratio)
		ladder[countInk(c, 0)] = true
	}
	for i := 0; i <= 1000; i++ {
		ratio := float64(i) / 1000
		c := render.NewCanvas(image.Rect(0, 0, 64, 64), fourInk)
		c.Fill(epaper.White)
		c.Dither(c.Bounds(), epaper.Black, epaper.White, ratio)
		if n := countInk(c, 0); !ladder[n] {
			t.Fatalf("ratio %v inked %d pixels, which no level produces", ratio, n)
		}
	}
}

// The reporter's own case: 0.18 was chosen by squinting, and is identical to
// anything else in its band. Snapping tells them which rung they picked.
func TestNearestDitherLevel(t *testing.T) {
	for _, tc := range []struct {
		in   float64
		want float64
	}{
		{0.18, 3.0 / 16}, // the reporter's value
		{0.157, 3.0 / 16},
		{0.20, 3.0 / 16},
		{0, 0},
		{1, 1},
		{0.5, 0.5},
		{-3, 0},   // clamped, like Dither itself
		{99, 1},   //
		{0.03, 0}, // nearer 0 than 1/16
	} {
		if got := render.NearestDitherLevel(tc.in); got != tc.want {
			t.Errorf("NearestDitherLevel(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// Snapping must never change the picture: the level it returns has to render
// identically to the ratio it was given.
func TestNearestDitherLevelDrawsTheSame(t *testing.T) {
	for i := 0; i <= 200; i++ {
		ratio := float64(i) / 200
		snapped := render.NearestDitherLevel(ratio)

		a := render.NewCanvas(image.Rect(0, 0, 64, 64), fourInk)
		a.Fill(epaper.White)
		a.Dither(a.Bounds(), epaper.Black, epaper.White, ratio)

		b := render.NewCanvas(image.Rect(0, 0, 64, 64), fourInk)
		b.Fill(epaper.White)
		b.Dither(b.Bounds(), epaper.Black, epaper.White, snapped)

		if string(a.Image().Pix) != string(b.Image().Pix) {
			t.Fatalf("ratio %v snapped to %v, which draws differently", ratio, snapped)
		}
	}
}

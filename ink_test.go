package epaper_test

import (
	"image/color"
	"testing"

	"github.com/sweeney/epaper"
)

func TestInkString(t *testing.T) {
	for _, tc := range []struct {
		ink  epaper.Ink
		want string
	}{
		{epaper.Black, "black"},
		{epaper.White, "white"},
		{epaper.Red, "red"},
		{epaper.Yellow, "yellow"},
		{epaper.Green, "green"},
		{epaper.Blue, "blue"},
		{epaper.Orange, "orange"},
		{epaper.Ink(200), "ink(200)"},
	} {
		if got := tc.ink.String(); got != tc.want {
			t.Errorf("Ink(%d).String() = %q, want %q", tc.ink, got, tc.want)
		}
	}
}

// An Ink's nominal RGB is what NearestTo measures against. It is deliberately
// NOT any particular panel's rendition — that lives in a Palette Entry.
func TestInkNominalRGBIsDistinct(t *testing.T) {
	seen := map[[3]uint8]epaper.Ink{}
	for _, ink := range []epaper.Ink{
		epaper.Black, epaper.White, epaper.Red,
		epaper.Yellow, epaper.Green, epaper.Blue, epaper.Orange,
	} {
		c := ink.RGB()
		if c.A != 255 {
			t.Errorf("%s: alpha = %d, want 255 — nominal inks are opaque", ink, c.A)
		}
		k := [3]uint8{c.R, c.G, c.B}
		if prev, dup := seen[k]; dup {
			t.Errorf("%s and %s share nominal RGB %v; NearestTo could not tell them apart", ink, prev, k)
		}
		seen[k] = ink
	}
}

// An Ink value outside the declared set must degrade, not index past the
// table. Both the name and the nominal colour have to cope.
func TestInkOutOfRange(t *testing.T) {
	for _, ink := range []epaper.Ink{0xFF, 100, 7} {
		if got := ink.RGB(); got != (color.RGBA{0, 0, 0, 255}) {
			t.Errorf("Ink(%d).RGB() = %v, want opaque black", ink, got)
		}
		if got := ink.String(); got == "" {
			t.Errorf("Ink(%d).String() is empty", ink)
		}
	}
	if got := epaper.Ink(0xFF).String(); got != "ink(255)" {
		t.Errorf("Ink(255).String() = %q, want %q", got, "ink(255)")
	}
}

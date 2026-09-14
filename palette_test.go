package epaper_test

import (
	"errors"
	"image/color"
	"testing"

	"github.com/sweeney/epaper"
)

// fourInk mirrors the JD79668 palette: black, white, yellow, red, in that
// order. Order is wire-significant — see TestPaletteOrderIsWireSignificant.
var fourInk = epaper.Palette{
	{Ink: epaper.Black, RGB: color.RGBA{0, 0, 0, 255}},
	{Ink: epaper.White, RGB: color.RGBA{255, 255, 255, 255}},
	{Ink: epaper.Yellow, RGB: color.RGBA{235, 205, 40, 255}},
	{Ink: epaper.Red, RGB: color.RGBA{190, 45, 40, 255}},
}

func TestPaletteIndex(t *testing.T) {
	for _, tc := range []struct {
		ink   epaper.Ink
		want  uint8
		found bool
	}{
		{epaper.Black, 0, true},
		{epaper.White, 1, true},
		{epaper.Yellow, 2, true},
		{epaper.Red, 3, true},
		// The whole point: an absent ink reports absent. It does NOT come
		// back as a plausible-looking index.
		{epaper.Green, 0, false},
		{epaper.Blue, 0, false},
		{epaper.Orange, 0, false},
	} {
		got, ok := fourInk.Index(tc.ink)
		if ok != tc.found {
			t.Errorf("Index(%s) found = %v, want %v", tc.ink, ok, tc.found)
			continue
		}
		if ok && got != tc.want {
			t.Errorf("Index(%s) = %d, want %d", tc.ink, got, tc.want)
		}
		if !ok && got != 0 {
			t.Errorf("Index(%s) returned index %d alongside found=false; an absent ink must not hand back a usable-looking index", tc.ink, got)
		}
	}
}

func TestPaletteHas(t *testing.T) {
	if !fourInk.Has(epaper.Red) {
		t.Error("Has(red) = false on a palette containing red")
	}
	if fourInk.Has(epaper.Green) {
		t.Error("Has(green) = true on a four-ink palette")
	}
}

// Slice position IS the index sent to the controller. A driver author
// reordering the palette for tidiness would silently swap the panel's
// colours, so the order is pinned here. PLAN §9.1.
func TestPaletteOrderIsWireSignificant(t *testing.T) {
	want := []epaper.Ink{epaper.Black, epaper.White, epaper.Yellow, epaper.Red}
	if len(fourInk) != len(want) {
		t.Fatalf("palette length = %d, want %d", len(fourInk), len(want))
	}
	for i, ink := range want {
		if fourInk[i].Ink != ink {
			t.Errorf("palette[%d] = %s, want %s — this is the byte the controller receives", i, fourInk[i].Ink, ink)
		}
	}
}

func TestPaletteColors(t *testing.T) {
	got := fourInk.Colors()
	if len(got) != 4 {
		t.Fatalf("Colors() length = %d, want 4", len(got))
	}
	for i, e := range fourInk {
		if got[i] != color.Color(e.RGB) {
			t.Errorf("Colors()[%d] = %v, want %v", i, got[i], e.RGB)
		}
	}
}

// Colors() must hand back a copy: a caller mutating the result (image.Paletted
// lets you) must not reach back into the driver's package-level palette.
func TestPaletteColorsIsACopy(t *testing.T) {
	got := fourInk.Colors()
	got[0] = color.RGBA{1, 2, 3, 4}
	if again := fourInk.Colors(); again[0] == color.Color(color.RGBA{1, 2, 3, 4}) {
		t.Error("mutating the result of Colors() changed the palette itself")
	}
}

func TestPaletteNearestTo(t *testing.T) {
	for _, tc := range []struct {
		ask, want epaper.Ink
	}{
		// Present inks resolve to themselves, exactly.
		{epaper.Red, epaper.Red},
		{epaper.White, epaper.White},
		// Absent inks fall back to the closest rendition this panel has.
		// Orange and green both land on this panel's warm yellow, and blue
		// on black; those are the distances, not a preference.
		{epaper.Orange, epaper.Yellow},
		{epaper.Green, epaper.Yellow},
		{epaper.Blue, epaper.Black},
	} {
		if got := fourInk.NearestTo(tc.ask); got != tc.want {
			t.Errorf("NearestTo(%s) = %s, want %s", tc.ask, got, tc.want)
		}
	}
}

// An empty palette knows nothing, so the honest answer is "the ink you asked
// for" — Index will then report it absent, which surfaces the problem at the
// point that can actually act on it. Returning Black would be a silent lie.
func TestPaletteNearestToOnEmptyPalette(t *testing.T) {
	var empty epaper.Palette
	if got := empty.NearestTo(epaper.Green); got != epaper.Green {
		t.Errorf("empty.NearestTo(green) = %s, want green", got)
	}
}

func TestPaletteValidate(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    epaper.Palette
		want error
	}{
		{"good", fourInk, nil},
		{"empty", epaper.Palette{}, epaper.ErrBadPalette},
		{"duplicate ink", epaper.Palette{
			{Ink: epaper.Black, RGB: color.RGBA{0, 0, 0, 255}},
			{Ink: epaper.Black, RGB: color.RGBA{9, 9, 9, 255}},
		}, epaper.ErrBadPalette},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.p.Validate()
			if !errors.Is(err, tc.want) {
				t.Errorf("Validate() = %v, want %v", err, tc.want)
			}
		})
	}
}

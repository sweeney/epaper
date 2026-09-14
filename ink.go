package epaper

import "image/color"

// Ink is which colour you mean. It is an identity, not a value: universal and
// panel-independent, so the same [Red] means the same thing whatever is
// plugged in.
//
// What a particular panel can actually produce is a [Palette], declared by its
// driver. Keeping the two apart is what stops one panel's capabilities from
// being baked into the whole library — see the package documentation.
type Ink uint8

// The inks this library knows how to name. A panel supports some subset; ask
// its [Palette] which.
//
// Adding to this list is not a breaking change, but reordering it is not
// either — these values are never sent to hardware. The wire index comes from
// the palette's slice position, not from here.
const (
	Black Ink = iota
	White
	Red
	Yellow
	Green
	Blue
	Orange
)

// inkNames is indexed by Ink.
var inkNames = [...]string{
	Black:  "black",
	White:  "white",
	Red:    "red",
	Yellow: "yellow",
	Green:  "green",
	Blue:   "blue",
	Orange: "orange",
}

// nominalRGB is the textbook rendition of each ink, used only as the reference
// point for [Palette.NearestTo]. It is deliberately not any real panel's
// output — a wHAT's red and a Spectra 6's red are visibly different, and both
// are some distance from pure #FF0000. A panel's own rendition lives in its
// [Entry].
var nominalRGB = [...]color.RGBA{
	Black:  {0, 0, 0, 255},
	White:  {255, 255, 255, 255},
	Red:    {255, 0, 0, 255},
	Yellow: {255, 255, 0, 255},
	Green:  {0, 255, 0, 255},
	Blue:   {0, 0, 255, 255},
	Orange: {255, 128, 0, 255},
}

// String returns the ink's lower-case name, or a placeholder for a value that
// is not one of the declared inks.
func (i Ink) String() string {
	if int(i) < len(inkNames) {
		return inkNames[i]
	}
	return "ink(" + itoa(uint8(i)) + ")"
}

// RGB returns the ink's nominal colour: the textbook version, not any
// particular panel's rendition. It exists so [Palette.NearestTo] has something
// to measure against. For what a panel will really put on the glass, read the
// [Entry] in its palette.
//
// An undeclared ink reports opaque black.
func (i Ink) RGB() color.RGBA {
	if int(i) < len(nominalRGB) {
		return nominalRGB[i]
	}
	return color.RGBA{0, 0, 0, 255}
}

// itoa formats a small unsigned value without pulling strconv (and therefore
// most of fmt) into a package that is otherwise this small.
func itoa(v uint8) string {
	if v == 0 {
		return "0"
	}
	var buf [3]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

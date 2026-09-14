package epaper

import (
	"fmt"
	"image/color"
)

// Entry is one ink a panel can produce, together with that panel's rendition
// of it.
//
// RGB is for previews and PNG output only — it is never sent to the
// controller, which is told an index. Two panels can both have [Red] and
// render it quite differently; that difference lives here.
type Entry struct {
	// Ink is which colour this entry is.
	Ink Ink
	// RGB is how this panel renders that ink, for previews and PNG output.
	RGB color.RGBA
}

// Palette is what a given panel can do, declared by its driver.
//
// # Order is wire-significant
//
// A palette's slice position IS the index sent to the controller. Element 0 is
// the byte value 0 on the wire, element 1 the value 1, and so on. Reordering a
// driver's palette — to sort it, to tidy it, to group the accent inks — would
// silently swap the colours on the panel, and every test would still pass
// unless one pins the order. Drivers should pin theirs.
//
// Use [Palette.Index] to resolve an ink; never write an integer index by hand.
type Palette []Entry

// Index returns the wire index for an ink, and whether this palette has it.
//
// When the ink is absent the returned index is 0 and ok is false. Do not use
// the index in that case — 0 is a real, usable index (typically black), so a
// caller that ignores ok will quietly draw the wrong colour. That is precisely
// the failure this two-value form exists to prevent.
func (p Palette) Index(ink Ink) (idx uint8, ok bool) {
	for i, e := range p {
		if e.Ink == ink {
			return uint8(i), true
		}
	}
	return 0, false
}

// Has reports whether this palette contains the given ink.
func (p Palette) Has(ink Ink) bool {
	_, ok := p.Index(ink)
	return ok
}

// Colors returns the palette as a [color.Palette], in wire order, ready for
// [image.NewPaletted].
//
// The result is a fresh slice: callers may mutate it — and image.Paletted
// invites them to — without reaching back into the driver's palette.
func (p Palette) Colors() color.Palette {
	out := make(color.Palette, len(p))
	for i, e := range p {
		out[i] = e.RGB
	}
	return out
}

// NearestTo returns the ink in this palette that best approximates the one
// asked for, which is lossy by definition: asking a four-ink panel for green
// gets you whichever of its four inks is least wrong, not green.
//
// Prefer [Palette.Index] and handle the absent case deliberately. This is for
// callers who have decided a rough colour beats an error.
//
// An ink the palette already has resolves to itself exactly. An empty palette
// returns the ink unchanged — it has no basis for an opinion, and the
// subsequent [Palette.Index] will report the ink absent, which surfaces the
// problem somewhere it can be acted on.
func (p Palette) NearestTo(ink Ink) Ink {
	if len(p) == 0 || p.Has(ink) {
		return ink
	}
	want := ink.RGB()
	best, bestDist := p[0].Ink, colourDistance(want, p[0].RGB)
	for _, e := range p[1:] {
		if d := colourDistance(want, e.RGB); d < bestDist {
			best, bestDist = e.Ink, d
		}
	}
	return best
}

// Validate reports whether the palette is usable, wrapping [ErrBadPalette] if
// not. Drivers should call it on their own palette in a test; a palette that
// is empty or names the same ink twice is a driver bug, and an ambiguous
// palette resolves inks by whichever entry happens to come first.
func (p Palette) Validate() error {
	if len(p) == 0 {
		return fmt.Errorf("epaper: palette has no entries: %w", ErrBadPalette)
	}
	seen := make(map[Ink]int, len(p))
	for i, e := range p {
		if first, dup := seen[e.Ink]; dup {
			return fmt.Errorf("epaper: ink %s appears at both index %d and %d: %w", e.Ink, first, i, ErrBadPalette)
		}
		seen[e.Ink] = i
	}
	return nil
}

// colourDistance is the "redmean" approximation: cheap, needs no colour-space
// conversion, and tracks perceived difference markedly better than a plain
// Euclidean distance in sRGB, which over-weights blue.
//
// https://www.compuphase.com/cmetric.htm
func colourDistance(a, b color.RGBA) float64 {
	rmean := (float64(a.R) + float64(b.R)) / 2
	dr := float64(a.R) - float64(b.R)
	dg := float64(a.G) - float64(b.G)
	db := float64(a.B) - float64(b.B)
	return (2+rmean/256)*dr*dr + 4*dg*dg + (2+(255-rmean)/256)*db*db
}

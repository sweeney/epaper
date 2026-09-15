package render

import (
	"image"
	"image/color"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// ScaleFace returns a face that draws f at n times the size, by replicating
// each pixel into an n×n block.
//
// This is how to get large text out of a bitmap font, and on a display with no
// intermediate tones it is better than it sounds: an integer-scaled bitmap
// glyph is exactly as crisp as the original, because every edge stays on a
// pixel boundary. A scaled outline font at the same size is smoother in
// principle and worse in practice, since its edges land between pixels and
// have nowhere to go — see [FontFamily].
//
// n must be at least 1; smaller values return f unchanged. Scaling an outline
// face works too, but there is rarely a reason: outline faces can simply be
// built at the size you want.
func ScaleFace(f font.Face, n int) font.Face {
	if f == nil || n <= 1 {
		return f
	}
	return &scaledFace{base: f, n: n}
}

type scaledFace struct {
	base font.Face
	n    int
}

func (s *scaledFace) Close() error { return s.base.Close() }

func (s *scaledFace) Kern(r0, r1 rune) fixed.Int26_6 {
	return s.base.Kern(r0, r1) * fixed.Int26_6(s.n)
}

func (s *scaledFace) GlyphAdvance(r rune) (fixed.Int26_6, bool) {
	adv, ok := s.base.GlyphAdvance(r)
	return adv * fixed.Int26_6(s.n), ok
}

func (s *scaledFace) GlyphBounds(r rune) (fixed.Rectangle26_6, fixed.Int26_6, bool) {
	b, adv, ok := s.base.GlyphBounds(r)
	n := fixed.Int26_6(s.n)
	b.Min.X *= n
	b.Min.Y *= n
	b.Max.X *= n
	b.Max.Y *= n
	return b, adv * n, ok
}

func (s *scaledFace) Metrics() font.Metrics {
	m := s.base.Metrics()
	n := fixed.Int26_6(s.n)
	m.Height *= n
	m.Ascent *= n
	m.Descent *= n
	m.XHeight *= n
	m.CapHeight *= n
	return m
}

// Glyph asks the base face for the glyph at the origin, then scales the
// resulting rectangle and mask and translates them to the real dot.
//
// Going via the origin rather than passing the scaled dot straight through
// keeps the sub-pixel rounding in one place: the base face rounds once, and
// everything after that is integer multiplication. Rounding at the scaled
// size instead would put glyph origins at positions the n×n blocks cannot
// land on, which is exactly the misalignment this whole function exists to
// avoid.
func (s *scaledFace) Glyph(dot fixed.Point26_6, r rune) (image.Rectangle, image.Image, image.Point, fixed.Int26_6, bool) {
	bdr, bmask, bmaskp, badv, ok := s.base.Glyph(fixed.Point26_6{}, r)
	if !ok {
		return image.Rectangle{}, nil, image.Point{}, 0, false
	}

	n := s.n
	// Floor the dot to whole pixels; an arithmetic shift does that correctly
	// for negative coordinates too.
	ox, oy := int(dot.X>>6), int(dot.Y>>6)

	dr := image.Rect(
		ox+bdr.Min.X*n, oy+bdr.Min.Y*n,
		ox+bdr.Max.X*n, oy+bdr.Max.Y*n,
	)
	mask := &scaledMask{
		src:    bmask,
		origin: bmaskp,
		n:      n,
		size:   image.Pt(bdr.Dx()*n, bdr.Dy()*n),
	}
	return dr, mask, image.Point{}, badv * fixed.Int26_6(n), true
}

// scaledMask presents a glyph mask enlarged n times, without copying it.
type scaledMask struct {
	src    image.Image
	origin image.Point
	n      int
	size   image.Point
}

func (m *scaledMask) ColorModel() color.Model { return m.src.ColorModel() }

func (m *scaledMask) Bounds() image.Rectangle {
	return image.Rectangle{Max: m.size}
}

func (m *scaledMask) At(x, y int) color.Color {
	return m.src.At(m.origin.X+floorDiv(x, m.n), m.origin.Y+floorDiv(y, m.n))
}

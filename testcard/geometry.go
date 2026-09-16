package testcard

// The card's geometry, derived from the panel rather than hardcoded.
//
// # Why this exists
//
// The card was laid out for a 400x300 wHAT with every dimension written as a
// literal. On a 250x122 pHAT that did not degrade gracefully: the central disc
// fell off the bottom edge, the luminance ladders inverted because their
// bottom margin passed their top one, and the text ran out through the side.
// Not clipped — scrambled.
//
// So every position and size is now a fraction of the panel, and the
// fractions are written as integer ratios against the reference geometry:
// scaleX(23) means "23 pixels at 400 wide". At exactly 400x300 every one of
// them evaluates to the literal it replaced, which is what lets the reviewed
// 400x300 golden stand unchanged as proof that nothing moved.
//
// # What deliberately does NOT scale
//
// Anything measuring the panel's own pixel pitch: the dither matrix, the 1px
// checkers, the grating pitches, the 1px strokes and the font sizes. Those are
// the measurements the card exists to make, and they mean the same thing on
// every panel — a 3px grating scaled to 1px on a small panel would not be a
// smaller version of the same test, it would be a different and much harder
// one. A smaller panel therefore gets the same detail in a smaller frame,
// which is exactly what a person comparing two panels wants.

// The geometry the card was designed against. Every ratio below is expressed
// in these terms, and at this size the arithmetic is the identity.
const (
	refWidth  = 400
	refHeight = 300
)

// layout holds every dimension the card draws with, computed once from the
// panel's own size.
type layout struct {
	w, h int

	border int // castellation thickness
	castle int // castellation block pitch

	ladderTop    int // first ladder step
	ladderBottom int
	ladderBarW   int

	gratingInset  int // from the side edge
	gratingW      int
	gratingTop    int
	gratingH      int
	gratingBottom int // from the bottom edge, to the wedge's top

	patchHalfW      int // reference patch
	patchTop        int
	patchBottom     int
	patchInnerHalfW int
	patchInnerTop   int
	patchInnerBot   int

	wedgeTop    int
	wedgeBottom int
	wedgeInset  int // from the side edge
	wedgeW      int

	crossPitch int

	circleX, circleY, circleR int
}

// newLayout derives the card's geometry for a panel of the given size.
func newLayout(w, h int) layout {
	sx := func(n int) int { return w * n / refWidth }
	sy := func(n int) int { return h * n / refHeight }

	// The castellated border is an inset from all FOUR edges, so it follows
	// the smaller axis rather than the height. Scaling it with the height
	// alone makes it grow on a tall narrow panel while the width it has to fit
	// inside does not: at 100x400 a height-scaled border plus its ladder
	// reached x 22, past the step wedge at x 18.
	//
	// On both real panels the smaller axis IS the height, so this is the same
	// number it always was — 12 at 400x300, 4 at 250x122.
	sm := func(n int) int { return min(w, h) * n / refHeight }

	l := layout{
		w: w, h: h,

		border: atLeast(sm(12), 2),
		castle: atLeast(sm(25), 4),

		ladderTop:    sy(66),
		ladderBottom: h - sy(66),
		ladderBarW:   atLeast(sx(23), 6),

		gratingInset:  sx(40),
		gratingW:      atLeast(sx(52), 8),
		gratingTop:    sy(20),
		gratingH:      atLeast(sy(38), 8),
		gratingBottom: sy(70),

		patchHalfW:      sx(50),
		patchTop:        sy(22),
		patchBottom:     sy(53),
		patchInnerHalfW: sx(40),
		patchInnerTop:   sy(30),
		patchInnerBot:   sy(45),

		wedgeTop:    sy(94),
		wedgeBottom: sy(202),
		wedgeInset:  sx(74),
		wedgeW:      atLeast(sx(30), 6),

		// The crosshair tick pitch is a pixel measurement, not a layout one:
		// the ticks exist to be counted, and counting is easier when the
		// pitch is the same on every panel.
		crossPitch: 8,

		circleX: w / 2,
		circleY: sy(148),
		circleR: atLeast(sy(78), 8),
	}

	// The disc's radius comes from the panel's HEIGHT, but its centre comes
	// from the panel's width — which is only safe while the panel is wider
	// than it is tall. On a portrait geometry it is not: at 122x250 the
	// unclamped radius is 65 against a half-width of 61, and the disc is cut
	// off flat against both side edges.
	//
	// No panel this library drives is portrait today, so this is not reachable
	// through inky.Open. It is reachable through "epaper-testcard -size
	// 122x250", which exists precisely so an unsold geometry can be looked at
	// before anyone writes a driver for it — and a diagnostic card that is
	// itself clipped is worse than useless for that.
	//
	// The clamp is against the step wedges, which sit between the ladders and
	// the disc and are the innermost thing the disc could collide with. On a
	// landscape panel there is room for ladder, wedge, disc, wedge, ladder
	// across the width; at 122x250 there is not, and the only degradation that
	// keeps every element VISIBLE is a smaller disc. A card with a diagnostic
	// hidden under the disc is exactly as useless as one clipped by the edge.
	//
	// This is a no-op on both real panels — the limit works out to 94 and 92
	// against radii of 78 and 31 — so the reviewed goldens do not move.
	// TestLayoutIsUnchangedOnTheRealPanels pins that.
	if maxR := l.circleX - (l.wedgeInset + l.wedgeW) - 2; l.circleR > maxR {
		l.circleR = atLeast(maxR, 8)
	}
	return l
}

// circleBottom is the lowest y at which text may still be drawn inside the
// disc, with a margin so a descender cannot cross the outline.
func (l layout) circleBottom() int { return l.circleY + l.circleR - l.circleMargin() }

// circleMargin keeps text clear of the disc's outline. It scales, but never
// below a pixel or two, or a small panel's text touches the edge.
//
// It is not decoration: TextFitted grows text until it fills the box it is
// given, so whatever the half-width works out to is exactly how wide the text
// becomes. With too small a margin every line ends up against the outline and
// reads as though it has burst out of the circle.
func (l layout) circleMargin() int { return atLeast(l.circleR*10/78, 2) }

// inscribedHalfWidth is the half-width of the circle at a vertical offset dy
// from its centre, less the margin.
func (l layout) inscribedHalfWidth(dy int) int {
	d := l.circleR*l.circleR - dy*dy
	if d <= 0 {
		return 0
	}
	return isqrt(d) - l.circleMargin()
}

// atLeast floors a scaled dimension. A ratio that rounds to zero on a small
// panel would silently delete an element rather than shrink it.
func atLeast(v, min int) int {
	if v < min {
		return min
	}
	return v
}

// isqrt is an integer square root, so the layout has no floating point in it
// and is identical everywhere.
func isqrt(n int) int {
	x := 0
	for (x+1)*(x+1) <= n {
		x++
	}
	return x
}

func maxAbs(a, b int) int {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	return max(a, b)
}

package render

import (
	"fmt"
	"image"

	"github.com/sweeney/epaper"
	"golang.org/x/image/font"
)

// Align is where a line of text sits within its box horizontally.
type Align int

// The alignments. Left is the zero value, so the zero Align behaves like
// [Canvas.Text] and nothing changes meaning by being left unset.
const (
	AlignLeft Align = iota
	AlignCentre
	AlignRight
)

// String returns the alignment's name.
func (a Align) String() string {
	switch a {
	case AlignLeft:
		return "left"
	case AlignCentre:
		return "centre"
	case AlignRight:
		return "right"
	}
	return fmt.Sprintf("align(%d)", int(a))
}

// TextAligned draws one line of text in a rectangle, placed horizontally by a.
//
// [Canvas.Text] takes a top-left point, which is the right primitive but the
// wrong ergonomics for the thing dashboards do constantly: a numeric column, a
// clock, an axis label — all of them right-aligned against something, all of
// them otherwise written as
//
//	c.Text(image.Pt(right-MeasureText(s, f), y), s, f, ink)
//
// six lines at a time, in every consumer, as issue #5 pointed out. The
// overflow axis has three answers now ([Canvas.TextFitted],
// [Canvas.TextWrapped], [Canvas.TextTruncated]); this is the placement axis
// having more than one.
//
// The text is drawn at the TOP of r, on one line; r's height is not used to
// centre vertically, because a caller who wants that is really placing a box
// and can say so. It composes with truncation, which is the combination a
// fixed-size dashboard wants:
//
//	c.TextAligned(box, TruncateText(s, f, box.Dx()), f, ink, AlignRight)
//
// If the text is WIDER than r, alignment is meaningless — there is no room to
// move it — so it starts at r's left edge. That is deliberate: a naive
// right-align would place it at a negative offset and walk a clock off the
// side of the panel, silently, on a display nobody is watching. Truncate or
// widen the box instead.
//
// An unrecognised Align is recorded as an error rather than treated as left,
// because a wrong constant is a bug and drawing something plausible hides it.
func (c *Canvas) TextAligned(r image.Rectangle, s string, f font.Face, i epaper.Ink, a Align) {
	if _, ok := c.ink(i); !ok {
		return
	}
	if f == nil {
		c.fail(fmt.Errorf("render: text aligned: font face is nil"))
		return
	}
	if s == "" {
		return
	}

	w := MeasureText(s, f)
	var x int
	switch a {
	case AlignLeft:
		x = r.Min.X
	case AlignCentre:
		x = r.Min.X + (r.Dx()-w)/2
	case AlignRight:
		x = r.Max.X - w
	default:
		c.fail(fmt.Errorf("render: text aligned: %v is not a known alignment", a))
		return
	}
	if x < r.Min.X {
		// Wider than the box; see the doc comment.
		x = r.Min.X
	}
	c.Text(image.Pt(x, r.Min.Y), s, f, i)
}

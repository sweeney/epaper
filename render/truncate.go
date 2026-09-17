package render

import (
	"fmt"
	"image"

	"github.com/sweeney/epaper"
	"golang.org/x/image/font"
)

// DefaultEllipsis is the marker [TruncateText] appends to text it has cut.
//
// Three full stops rather than U+2026, because [basicfont.Face7x13] — the face
// the bundled family returns below 17px — has no ellipsis glyph, and a missing
// glyph draws as a blank or a box. Using the real character would therefore
// look like a font fault at exactly the size where text is hardest to read
// anyway. Inconsolata does have it; a marker that changed with the size would
// be worse than one that is merely plain, because a dashboard shows several
// sizes at once.
//
// Use [TruncateTextWith] to choose a different one.
const DefaultEllipsis = "..."

// TruncateText returns s shortened to fit within width pixels, with
// [DefaultEllipsis] marking the cut, or s unchanged if it already fits.
//
// This is the third answer to "the text does not fit", alongside
// [Canvas.TextFitted], which shrinks it, and [Canvas.TextWrapped], which runs
// it onto more lines. Reach for this one when the SIZE is fixed and the text
// may give — which is what a wall display usually wants, because a headline
// that changes size when the words change makes the whole screen look broken.
//
// If not even the marker fits, the result is empty: there is nothing that
// could be shown honestly in that space.
//
// Returns empty for a nil face, which cannot measure anything.
func TruncateText(s string, f font.Face, width int) string {
	return TruncateTextWith(s, f, width, DefaultEllipsis)
}

// TruncateTextWith is [TruncateText] with a marker of your choosing.
//
// An empty marker is allowed and means a hard cut with no indication, which is
// occasionally what a fixed-width column wants.
func TruncateTextWith(s string, f font.Face, width int, ellipsis string) string {
	if f == nil || width <= 0 {
		return ""
	}
	if MeasureText(s, f) <= width {
		return s
	}

	// Cut whole runes off the end until what is left, plus the marker, fits.
	// Indexing by rune rather than by byte is not fussiness: cutting a
	// multi-byte rune in half produces invalid UTF-8, which draws as a
	// replacement character and reads as a font bug rather than as a
	// truncation.
	runes := []rune(s)
	for n := len(runes); n > 0; n-- {
		if MeasureText(string(runes[:n])+ellipsis, f) <= width {
			return string(runes[:n]) + ellipsis
		}
	}
	// Even one character plus the marker is too wide. The marker alone is
	// still worth drawing if it fits — it says "there was something here".
	if MeasureText(ellipsis, f) <= width {
		return ellipsis
	}
	return ""
}

// TextTruncated draws one line of text at the top left of r, cut to r's width
// with an ellipsis, and returns what it actually drew.
//
// Unlike [Canvas.TextWrapped], truncating is NOT recorded as an error, and the
// difference is deliberate. The other overflow cases fail loudly because the
// text simply vanishes and nobody is watching the panel at the moment it
// happens. An ellipsis is visible on the glass: it tells the viewer that
// something was cut, which is the job the error was standing in for.
//
// It matters for the caller's tests too. A consumer whose regression check is
// "every fixture draws with Err() == nil" would otherwise see it fail whenever
// a headline ran long — which is precisely the case the ellipsis is for.
//
// A nil face, or an ink this panel does not have, is still an error.
func (c *Canvas) TextTruncated(r image.Rectangle, s string, f font.Face, i epaper.Ink) string {
	if _, ok := c.ink(i); !ok {
		return ""
	}
	if f == nil {
		c.fail(fmt.Errorf("render: text truncated: font face is nil"))
		return ""
	}
	out := TruncateText(s, f, r.Dx())
	if out == "" {
		return ""
	}
	c.Text(r.Min, out, f, i)
	return out
}

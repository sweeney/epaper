package render_test

import (
	"image"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/render"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/inconsolata"
)

// The dashboard case, from issue #4: a headline at a FIXED size that must not
// reflow between refreshes, cut with an ellipsis when it is too long.
//
// TextFitted is the wrong tool for it — a headline that shrinks when the words
// change makes a wall display look broken — and TextWrapped is the wrong tool
// because there is only room for one line.
func TestTruncateText(t *testing.T) {
	f := basicfont.Face7x13 // 7px per glyph, so the arithmetic is checkable by hand
	for _, tc := range []struct {
		name  string
		s     string
		width int
		want  string
	}{
		{"fits exactly", "ABCDE", 35, "ABCDE"},
		{"fits with room", "ABCDE", 100, "ABCDE"},
		{"empty stays empty", "", 100, ""},
		// 8 glyphs = 56px. At 49px we can afford 7 glyphs, three of which must
		// be the ellipsis, so four characters survive.
		{"cut with an ellipsis", "ABCDEFGH", 49, "ABCD..."},
		// Exactly enough for the ellipsis and one character.
		{"cut to almost nothing", "ABCDEFGH", 28, "A..."},
		// Not even the ellipsis fits, so nothing can be said honestly.
		{"no room at all", "ABCDEFGH", 20, ""},
		{"zero width", "ABCDEFGH", 0, ""},
		{"negative width", "ABCDEFGH", -5, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := render.TruncateText(tc.s, f, tc.width)
			if got != tc.want {
				t.Errorf("TruncateText(%q, %d) = %q, want %q", tc.s, tc.width, got, tc.want)
			}
		})
	}
}

// The invariant that matters: whatever comes back must actually fit. A
// truncation helper that can still overflow is worse than none, because the
// caller has stopped checking.
func TestTruncateTextAlwaysFits(t *testing.T) {
	faces := map[string]font.Face{
		"basicfont":   basicfont.Face7x13,
		"inconsolata": inconsolata.Regular8x16,
		"scaled x3":   render.ScaleFace(inconsolata.Regular8x16, 3),
	}
	subjects := []string{
		"", "A", "WAIT IF YOU CAN", "13:08",
		"Red/Yellow wHAT (JD79668)",
		strings.Repeat("long ", 40),
		"héllo wörld with accents",
		"tabs\tand\nnewlines",
	}
	for name, f := range faces {
		for _, s := range subjects {
			for width := 0; width <= 200; width += 7 {
				got := render.TruncateText(s, f, width)
				if w := render.MeasureText(got, f); w > width {
					t.Fatalf("%s: TruncateText(%q, %d) = %q, which measures %d", name, s, width, got, w)
				}
				if len(got) > len(s)+len("...") {
					t.Fatalf("%s: TruncateText(%q, %d) = %q, longer than the input", name, s, width, got)
				}
			}
		}
	}
}

// Multi-byte runes must not be cut in half: that yields invalid UTF-8, which
// draws as a replacement character and reads as a font bug rather than as a
// truncation.
//
// Read this as a GUARD, not as proof. Slicing []byte instead of []rune passes
// it, and that is not a hole in the test — it is a fact about the faces. Go
// ranges each stray byte as one U+FFFD, and on every face reachable from this
// module U+FFFD is at least as wide as the rune it came from (7px against 7px
// on the bitmap faces; 20px against 12px for a euro sign on goregular). A
// half-cut prefix therefore always measures MORE than the clean cut just below
// it, so the loop never selects one. The bug is real but unreachable here.
//
// []rune is used anyway, because it is correct for ANY face metrics rather
// than for the ones that happen to be linked in, and this test exists so that
// a future implementation which is not correct by construction gets caught.
func TestTruncateTextDoesNotSplitRunes(t *testing.T) {
	f := basicfont.Face7x13
	for _, s := range []string{
		"\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9",
		"\u20ac\u20ac\u20ac\u20ac\u20ac\u20ac\u20ac\u20ac\u20ac\u20ac\u20ac",
		"\u4e16\u754c\u4e16\u754c\u4e16\u754c\u4e16\u754c",
		"a\u20acb\u4e16c\u20acd",
	} {
		for width := range 120 {
			got := render.TruncateText(s, f, width)
			if !utf8.ValidString(got) {
				t.Fatalf("TruncateText(%q, %d) = %q, which is not valid UTF-8", s, width, got)
			}
		}
	}
}

// A nil face cannot measure anything, so it must not pretend to.
func TestTruncateTextNilFace(t *testing.T) {
	if got := render.TruncateText("hello", nil, 100); got != "" {
		t.Errorf("TruncateText with a nil face = %q, want empty", got)
	}
}

// The ellipsis is configurable, because "..." is right for the bundled bitmap
// faces and wrong for a face that has a real one.
func TestTruncateTextWith(t *testing.T) {
	f := basicfont.Face7x13
	got := render.TruncateTextWith("ABCDEFGH", f, 49, ">")
	if got != "ABCDEF>" {
		t.Errorf("TruncateTextWith(...) = %q, want %q", got, "ABCDEF>")
	}
	// An empty marker is allowed: a hard cut with no indication.
	if got := render.TruncateTextWith("ABCDEFGH", f, 49, ""); got != "ABCDEFG" {
		t.Errorf("with an empty marker = %q, want %q", got, "ABCDEFG")
	}
}

// Canvas.TextTruncated draws one line, cut to the rectangle.
func TestCanvasTextTruncated(t *testing.T) {
	c := render.NewCanvas(image.Rect(0, 0, 60, 20), fourInk)
	c.Fill(epaper.White)

	got := c.TextTruncated(image.Rect(0, 0, 49, 13), "ABCDEFGH", basicfont.Face7x13, epaper.Black)
	if got != "ABCD..." {
		t.Errorf("TextTruncated returned %q, want %q", got, "ABCD...")
	}
	if err := c.Err(); err != nil {
		t.Errorf("Err() = %v — truncation is expected behaviour, not a failure", err)
	}
	if countInk(c, 0) == 0 {
		t.Error("nothing was drawn")
	}
}

// Truncating must NOT record an error, and that is a deliberate departure from
// TextWrapped. The reason is on the panel: an ellipsis is visible, so the
// viewer can see that something was cut. Clipped text just vanishes, which is
// what the error exists to substitute for.
//
// It matters practically too: a consumer whose regression test is "every
// fixture draws with Err() == nil" would otherwise fail whenever a headline
// happened to be long, which is exactly when they want the ellipsis.
func TestTextTruncatedIsNotAnError(t *testing.T) {
	c := render.NewCanvas(image.Rect(0, 0, 400, 40), fourInk)
	c.Fill(epaper.White)
	for _, s := range []string{"short", strings.Repeat("very long headline ", 10)} {
		c.TextTruncated(image.Rect(0, 0, 100, 13), s, basicfont.Face7x13, epaper.Black)
	}
	if err := c.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
}

func TestTextTruncatedNilFace(t *testing.T) {
	c := render.NewCanvas(image.Rect(0, 0, 60, 20), fourInk)
	if got := c.TextTruncated(image.Rect(0, 0, 50, 13), "x", nil, epaper.Black); got != "" {
		t.Errorf("returned %q, want empty", got)
	}
	if c.Err() == nil {
		t.Error("a nil face must be reported")
	}
}

// An ink the panel does not have is still an error, like everywhere else.
func TestTextTruncatedMissingInk(t *testing.T) {
	c := render.NewCanvas(image.Rect(0, 0, 60, 20), fourInk)
	c.TextTruncated(image.Rect(0, 0, 50, 13), "x", basicfont.Face7x13, epaper.Green)
	if c.Err() == nil {
		t.Error("drawing in an ink the palette lacks must be reported")
	}
}

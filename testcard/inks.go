package testcard

// This file holds the inks card; the package doc is in testcard.go.

import (
	"image"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/render"
	"golang.org/x/image/font"
)

// DrawInks paints every ink the canvas has, and every pair of them mixed.
//
// It is a square matrix with one row and one column per ink, in palette
// order, each row labelled with its ink's name:
//
//	diagonal         the ink on its own, solid
//	above it         a 1px checker of the row's ink and the column's
//	below it         a 50% ordered dither of the row's ink over the column's
//
// The diagonal is what it is for. A driver that sends the wrong wire value for
// an ink — easy on a controller like the E640, whose codes skip one — draws a
// perfectly tidy card with a swatch in the wrong colour, and the label beside
// it is the only thing on the glass that says so. Nothing else in this
// package puts a name next to an ink.
//
// The mixes are the second question: how two inks look interleaved at the
// panel's finest pitch, which is what every dithered tone on the other cards
// is made of, and whether any pair bleeds into a third colour.
//
// Like [DrawOrientation] it is drawn from the canvas's own bounds and palette,
// so it works on any panel; labels are left off when there is no room for
// them.
func DrawInks(c *render.Canvas) {
	p := c.Palette()
	b := c.Bounds()
	g := newInkGrid(b.Dx(), b.Dy(), len(p), Fonts())

	c.Fill(epaper.White)
	if g.pitch < 1 {
		return
	}

	for row, re := range p {
		for col, ce := range p {
			r := g.cell(row, col).Add(b.Min)
			switch {
			case row == col:
				c.Rect(r, re.Ink)
			case col > row:
				c.Checker(r, re.Ink, ce.Ink, 1)
			default:
				c.Dither(r, re.Ink, ce.Ink, 0.5)
			}
		}
		if g.face != nil {
			top := g.cell(row, 0).Add(b.Min)
			name := re.Ink.String()
			x := top.Min.X - inkLabelGap - render.MeasureText(name, g.face)
			y := top.Min.Y + (top.Dy()-render.LineHeight(g.face))/2
			c.Text(image.Pt(x, y), name, g.face, epaper.Black)
		}
	}
}

// inkLabelSize is the label face's size: the smallest bundled bitmap, which is
// legible on every panel and leaves the most room for the swatches.
const inkLabelSize = 13

// inkLabelGap separates a row's label from its first cell.
const inkLabelGap = 4

// inkGrid is where the inks card puts things. It is separate from the drawing
// so a test can find a cell without re-deriving the layout.
type inkGrid struct {
	x0, y0 int // top-left of the first cell
	pitch  int // cell size including its gutter
	gutter int // white space between cells
	face   font.Face
}

// newInkGrid lays out an n-by-n matrix on a w-by-h panel, leaving a column on
// the left for labels when there is room for one. The matrix is square, as
// large as fits, and centred in what is left.
func newInkGrid(w, h, n int, fonts render.FontFamily) inkGrid {
	var g inkGrid
	if n < 1 {
		return g
	}

	labelW := 0
	if fonts != nil {
		if f, err := fonts(inkLabelSize); err == nil {
			for _, name := range []string{"yellow", "orange", "magenta"} {
				labelW = max(labelW, render.MeasureText(name, f))
			}
			labelW += 2 * inkLabelGap
			// Labels only if the matrix left over still has cells worth
			// looking at, and a row is tall enough to carry a line of text.
			pitch := min((w-labelW)/n, h/n)
			if pitch >= render.LineHeight(f) {
				g.face = f
			} else {
				labelW = 0
			}
		}
	}

	g.pitch = min((w-labelW)/n, h/n)
	if g.pitch >= 12 {
		g.gutter = 2
	}
	g.x0 = labelW + (w-labelW-n*g.pitch)/2
	g.y0 = (h - n*g.pitch) / 2
	return g
}

// cell returns the rectangle for a row and column, without its gutter,
// relative to the panel's origin.
func (g inkGrid) cell(row, col int) image.Rectangle {
	x := g.x0 + col*g.pitch
	y := g.y0 + row*g.pitch
	return image.Rect(x, y, x+g.pitch-g.gutter, y+g.pitch-g.gutter)
}

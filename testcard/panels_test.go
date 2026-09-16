package testcard_test

import (
	"fmt"
	"image"
	"image/color"
	"testing"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/inky"
	"github.com/sweeney/epaper/internal/golden"
	"github.com/sweeney/epaper/render"
	"github.com/sweeney/epaper/testcard"
)

// Every panel the library supports gets its card and its orientation card
// rendered, and both go into testdata/golden — which is what CI collects into
// the HTML report. Adding a driver therefore adds its renders to the report
// without anyone having to remember to.
//
// This is an external test package (testcard_test, not testcard) because it
// imports inky, and inky imports the driver packages. Keeping it out here
// means the library's own dependency graph is unchanged: testcard still knows
// nothing about any particular board.
//
// LOOK AT THESE when they change. A golden accepted without being looked at
// asserts nothing at all — and the whole reason this file exists is that the
// card was scrambled at 250x122 for as long as nobody had rendered it there.
var fourInk = epaper.Palette{
	{Ink: epaper.Black, RGB: color.RGBA{0, 0, 0, 255}},
	{Ink: epaper.White, RGB: color.RGBA{255, 255, 255, 255}},
	{Ink: epaper.Yellow, RGB: color.RGBA{235, 205, 40, 255}},
	{Ink: epaper.Red, RGB: color.RGBA{190, 45, 40, 255}},
}

func TestGoldenEverySupportedPanel(t *testing.T) {
	panels := inky.SupportedPanels()
	if len(panels) == 0 {
		t.Fatal("no supported panels; this test would assert nothing")
	}

	for _, p := range panels {
		t.Run(fmt.Sprintf("%dx%d", p.Width, p.Height), func(t *testing.T) {
			bounds := image.Rect(0, 0, p.Width, p.Height)

			card := render.NewCanvas(bounds, fourInk)
			// Fixed lines, so the goldens do not change with the clock.
			testcard.Draw(card, testcard.Options{Lines: []string{p.Model, fmt.Sprintf("%dx%d 4-ink", p.Width, p.Height)}})
			if err := card.Err(); err != nil {
				t.Fatalf("Draw(): %v", err)
			}
			golden.Assert(t, fmt.Sprintf("panel-%dx%d-testcard", p.Width, p.Height), card.Image())

			orient := render.NewCanvas(bounds, fourInk)
			testcard.DrawOrientation(orient)
			if err := orient.Err(); err != nil {
				t.Fatalf("DrawOrientation(): %v", err)
			}
			golden.Assert(t, fmt.Sprintf("panel-%dx%d-orientation", p.Width, p.Height), orient.Image())

			conf := render.NewCanvas(bounds, fourInk)
			testcard.DrawConformance(conf)
			if err := conf.Err(); err != nil {
				t.Fatalf("DrawConformance(): %v", err)
			}
			golden.Assert(t, fmt.Sprintf("panel-%dx%d-conformance", p.Width, p.Height), conf.Image())
		})
	}
}

// The card must actually fill the panel it is given. The failure this catches
// is the one that was really there: a layout of absolute pixel positions that
// leaves most of a small panel untouched, or piles everything into a corner.
func TestCardFillsEverySupportedPanel(t *testing.T) {
	for _, p := range inky.SupportedPanels() {
		t.Run(fmt.Sprintf("%dx%d", p.Width, p.Height), func(t *testing.T) {
			c := render.NewCanvas(image.Rect(0, 0, p.Width, p.Height), fourInk)
			testcard.Draw(c, testcard.Options{Lines: []string{p.Model}})
			if err := c.Err(); err != nil {
				t.Fatalf("Draw(): %v", err)
			}
			img := c.Image()

			// Every ink present, and the card reaching all four edges.
			seen := map[uint8]bool{}
			for y := range p.Height {
				for x := range p.Width {
					seen[img.ColorIndexAt(x, y)] = true
				}
			}
			if len(seen) != 4 {
				t.Errorf("used %d inks, want all 4", len(seen))
			}

			// The castellated border must reach every edge. If the layout
			// collapsed, an edge would be left showing the dithered field.
			for _, edge := range []struct {
				name string
				at   func(i int) uint8
				n    int
			}{
				{"top", func(i int) uint8 { return img.ColorIndexAt(i, 0) }, p.Width},
				{"bottom", func(i int) uint8 { return img.ColorIndexAt(i, p.Height-1) }, p.Width},
				{"left", func(i int) uint8 { return img.ColorIndexAt(0, i) }, p.Height},
				{"right", func(i int) uint8 { return img.ColorIndexAt(p.Width-1, i) }, p.Height},
			} {
				runs := 0
				for i := 1; i < edge.n; i++ {
					if edge.at(i) != edge.at(i-1) {
						runs++
					}
				}
				if runs < 2 {
					t.Errorf("%s edge has %d transitions; the castellation is not reaching it", edge.name, runs)
				}
			}
		})
	}
}

// Nothing drawn for the central disc may escape it, on ANY supported panel.
//
// The package's own containment test covers 400x300. That was not enough: at
// 250x122 the disc is about 56px at its widest, model names are wider than
// that, and the text went straight out through the side — which the 400x300
// test could not have seen. Sizes are where this bug lives, so the check is
// run at every size the library supports.
func TestCircleContentStaysInsideEverySupportedPanel(t *testing.T) {
	for _, p := range inky.SupportedPanels() {
		t.Run(fmt.Sprintf("%dx%d", p.Width, p.Height), func(t *testing.T) {
			bounds := image.Rect(0, 0, p.Width, p.Height)

			// The card as drawn, against the same card with the disc left
			// out: any pixel outside the disc that the two disagree about was
			// put there by the disc.
			full := render.NewCanvas(bounds, fourInk)
			testcard.Draw(full, testcard.Options{Lines: []string{p.Model, "a deliberately long second line"}})
			if err := full.Err(); err != nil {
				t.Fatalf("Draw(): %v", err)
			}
			bare := render.NewCanvas(bounds, fourInk)
			testcard.Draw(bare, testcard.Options{NoText: true})
			if err := bare.Err(); err != nil {
				t.Fatalf("Draw(NoText): %v", err)
			}

			// The disc's own geometry, recomputed here from the same ratios
			// the layout uses, so this test does not reach into the package.
			cx, cy := p.Width/2, p.Height*148/300
			r := p.Height * 78 / 300
			if r < 8 {
				r = 8
			}

			escaped := 0
			var first image.Point
			for y := range p.Height {
				for x := range p.Width {
					if full.Image().ColorIndexAt(x, y) == bare.Image().ColorIndexAt(x, y) {
						continue
					}
					dx, dy := x-cx, y-cy
					if dx*dx+dy*dy > r*r {
						if escaped == 0 {
							first = image.Pt(x, y)
						}
						escaped++
					}
				}
			}
			if escaped > 0 {
				t.Errorf("%d pixels of disc content landed outside the disc, first at %v", escaped, first)
			}
		})
	}
}

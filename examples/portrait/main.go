// Command portrait mounts a landscape panel on its end.
//
//	go run ./examples/portrait -png portrait.png   # no hardware needed
//	go run ./examples/portrait                     # draw on the panel
//
// # Why this is an example and not a library feature
//
// This library has no rotation. [epaper.Device.Bounds] reports the panel's
// native geometry — 250x122 for an Inky pHAT, 400x300 for a wHAT — and Show
// rejects an image of any other shape with [epaper.ErrWrongSize] before it
// touches the hardware. That is deliberate; PLAN.md §9.9 has the argument, and
// the short version is that rotation could reasonably live on the device, on
// the canvas, or as a render helper, and picking without a real use case is
// guesswork.
//
// So here is the whole of what a consumer has to do meanwhile. It is one
// nested loop, it is easy to get subtly wrong, and it is easier to check
// against this than to rederive.
//
// # Which way it turns
//
// rotateCW turns the CONTENT a quarter turn clockwise. To read it, you turn
// the PANEL the other way — anticlockwise. Those two sentences are the whole
// trap: the rotation applied and the rotation needed to undo it are opposites,
// and it is very easy to state one while meaning the other, which is a
// twenty-second refresh and a puzzled look each time.
//
// # It costs nothing
//
// The panel is the entire expense. Rotating a 250x122 frame is about 30,000
// pixel copies — microseconds — against an 18.5 second refresh.
//
// There is an irony worth knowing if you are tempted to push rotation down
// into the driver: the JD79661's controller frame is natively PORTRAIT,
// 128x250, and the driver already rotates it to present landscape. Asking for
// portrait therefore rotates twice and cancels out. A driver-level rotation
// option could skip both.
package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"time"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/inky"
	"github.com/sweeney/epaper/mock"
	"github.com/sweeney/epaper/render"
	"github.com/sweeney/epaper/testcard"
)

// panelPalette is the four-ink Inky palette, for the no-hardware path. On a Pi
// you get it from the device instead, and delete this.
var panelPalette = epaper.Palette{
	{Ink: epaper.Black, RGB: rgb(0, 0, 0)},
	{Ink: epaper.White, RGB: rgb(255, 255, 255)},
	{Ink: epaper.Yellow, RGB: rgb(235, 205, 40)},
	{Ink: epaper.Red, RGB: rgb(190, 45, 40)},
}

func main() {
	pngPath := flag.String("png", "", "render to this file instead of the panel")
	width := flag.Int("width", 250, "with -png: the PANEL's width (landscape)")
	height := flag.Int("height", 122, "with -png: the PANEL's height (landscape)")
	flag.Parse()

	if *pngPath != "" {
		dev := mock.New(*width, *height, panelPalette)
		defer func() { _ = dev.Close() }()
		if err := drawPortrait(context.Background(), dev); err != nil {
			log.Fatal(err)
		}
		if err := writePNG(*pngPath, dev.Last()); err != nil {
			log.Fatal(err)
		}
		fmt.Println("wrote", *pngPath)
		return
	}

	dev, err := inky.Open()
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = dev.Close() }()

	b := dev.Bounds()
	fmt.Printf("panel: %s %v -> drawing portrait %dx%d\n", dev.Model(), b, b.Dy(), b.Dx())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	started := time.Now()
	if err := drawPortrait(ctx, dev); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("done in %s — turn the panel ANTICLOCKWISE to read it "+
		"(the picture's top edge is along the panel's right-hand side)\n",
		time.Since(started).Round(time.Millisecond))
}

// drawPortrait draws the test card as though the panel were stood on its end.
//
// The only thing specific to portrait is the swapped canvas and the rotateCW
// call; everything between them is ordinary drawing against ordinary bounds,
// which is the point. Swap testcard.Draw for your own layout.
func drawPortrait(ctx context.Context, dev epaper.Device) error {
	b := dev.Bounds()
	pw, ph := b.Dy(), b.Dx() // the panel, stood on its end

	c := render.NewCanvas(image.Rect(0, 0, pw, ph), dev.Palette())
	testcard.Draw(c, testcard.Options{Lines: []string{
		fmt.Sprintf("portrait %dx%d", pw, ph),
		dev.Model(),
	}})
	if err := c.Err(); err != nil {
		return fmt.Errorf("drawing: %w", err)
	}

	return dev.Show(ctx, rotateCW(c.Image(), dev.Palette()))
}

// rotateCW returns src turned a quarter turn clockwise.
//
// The result's width is src's height and vice versa, so feeding it a portrait
// image of a landscape panel's transposed bounds gives back something Show
// will accept.
//
// The mapping, which is the part worth stating rather than rediscovering:
//
//	dst(x, y) = src(y, srcHeight-1-x)
//
// so src's top-left corner ends up at dst's top-RIGHT. Check it on a corner
// whenever you touch it; every rotation is plausible-looking and only one is
// right, and on a panel the feedback loop is twenty seconds long.
func rotateCW(src *image.Paletted, palette epaper.Palette) *image.Paletted {
	sb := src.Bounds()
	dst := image.NewPaletted(image.Rect(0, 0, sb.Dy(), sb.Dx()), palette.Colors())
	for y := range sb.Dx() {
		for x := range sb.Dy() {
			dst.SetColorIndex(x, y, src.ColorIndexAt(sb.Min.X+y, sb.Max.Y-1-x))
		}
	}
	return dst
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func rgb(r, g, b uint8) color.RGBA { return color.RGBA{r, g, b, 255} }

// Command epaper-testcard draws a test pattern, either on a real panel or to
// a PNG file.
//
// It is both a demo of the library and the hardware acceptance test: if the
// card looks right on the glass, the whole stack works.
//
//	epaper-testcard                     # draw the test card on the panel
//	epaper-testcard -pattern conformance # draw the conformance pattern
//	epaper-testcard -png card.png       # render to a file, no hardware needed
//
// The -png mode needs no Pi and no panel, which is the point: a layout can be
// checked in milliseconds rather than the 25 seconds a refresh costs.
//
// Note that this command embeds a font. The library deliberately does not —
// a font would dwarf it, and the choice belongs to the consumer — but a
// runnable demo has to draw text with something.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"time"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/inky"
	"github.com/sweeney/epaper/render"
	"github.com/sweeney/epaper/testcard"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "epaper-testcard:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		pngPath = flag.String("png", "", "render to this PNG file instead of the panel")
		pattern = flag.String("pattern", "testcard", "which pattern: testcard or conformance")
		note    = flag.String("note", "", "extra line of text on the card")
		timeout = flag.Duration("timeout", 2*time.Minute, "how long to wait for the refresh")
		fast    = flag.Bool("fast", false,
			"skip the vendor's 300ms per-command delay (PLAN §9.3 — look at the panel afterwards)")
	)
	flag.Parse()

	if *pattern != "testcard" && *pattern != "conformance" {
		return fmt.Errorf("unknown pattern %q: want testcard or conformance", *pattern)
	}

	// Off-panel: render against a stand-in of the real device, so the image
	// is identical to what the panel would be sent.
	if *pngPath != "" {
		img, err := draw(bounds400x300(), paletteForPNG(), *pattern, "offline render", *note)
		if err != nil {
			return err
		}
		return writePNG(*pngPath, img)
	}

	dev, err := inky.OpenWith(inky.Options{CommandDelay: commandDelay(*fast)})
	if err != nil {
		return err
	}
	defer func() { _ = dev.Close() }()

	fmt.Printf("panel: %s %v\n", dev.Model(), dev.Bounds())

	img, err := draw(dev.Bounds(), dev.Palette(), *pattern, dev.Model(), *note)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	fmt.Printf("drawing %s — a full refresh takes about 25 seconds...\n", *pattern)
	started := time.Now()
	if err := dev.Show(ctx, img); err != nil {
		return err
	}
	fmt.Printf("done in %s\n", time.Since(started).Round(time.Millisecond))
	return nil
}

// commandDelay turns -fast into the driver's convention, where a negative
// value means no delay at all and zero means "use the default".
func commandDelay(fast bool) time.Duration {
	if fast {
		return -1
	}
	return 0
}

func draw(bounds image.Rectangle, palette epaper.Palette, pattern, model, note string) (*image.Paletted, error) {
	c := render.NewCanvas(bounds, palette)
	switch pattern {
	case "conformance":
		testcard.DrawConformance(c)
	default:
		testcard.Draw(c, testcard.Options{Lines: []string{model, note}})
	}
	if err := c.Err(); err != nil {
		return nil, err
	}
	return c.Image(), nil
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", path)
	return nil
}

func bounds400x300() image.Rectangle { return image.Rect(0, 0, 400, 300) }

// paletteForPNG is the JD79668's palette. The offline path cannot ask a device
// for it, and hardcoding it here keeps the driver package out of a rendering
// command's import graph.
func paletteForPNG() epaper.Palette {
	return epaper.Palette{
		{Ink: epaper.Black, RGB: rgb(0, 0, 0)},
		{Ink: epaper.White, RGB: rgb(255, 255, 255)},
		{Ink: epaper.Yellow, RGB: rgb(235, 205, 40)},
		{Ink: epaper.Red, RGB: rgb(190, 45, 40)},
	}
}

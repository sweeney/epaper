// Command offline builds a layout with no hardware present.
//
//	go run ./examples/offline -o panel.png
//
// This is how to develop for e-ink. A refresh takes about 25 seconds and
// happens in another room; a PNG takes about 25 milliseconds and you can diff
// it. Get the layout right here, then change one line.
//
// The mock validates exactly as the real driver does — wrong size, wrong
// palette and a cancelled context all fail the same way — so a layout that
// works against it works against the panel.
package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"log"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/mock"
	"github.com/sweeney/epaper/render"
	"github.com/sweeney/epaper/testcard"
	"golang.org/x/image/font"
)

// panelPalette is the Inky wHAT's. Taking it from a constant rather than from
// a device is what lets this program run anywhere; on a Pi you would get it
// from inky.Open() instead and delete this.
var panelPalette = epaper.Palette{
	{Ink: epaper.Black, RGB: rgb(0, 0, 0)},
	{Ink: epaper.White, RGB: rgb(255, 255, 255)},
	{Ink: epaper.Yellow, RGB: rgb(235, 205, 40)},
	{Ink: epaper.Red, RGB: rgb(190, 45, 40)},
}

func main() {
	out := flag.String("o", "panel.png", "file to write")
	flag.Parse()

	// The ONLY line that differs from the real thing:
	//
	//	dev, err := inky.Open()
	//
	// Both satisfy epaper.Device, so nothing below this changes.
	dev := mock.New(400, 300, panelPalette)
	defer dev.Close()

	if err := drawAndShow(dev); err != nil {
		log.Fatal(err)
	}

	// The mock kept every frame it was shown. Write the last one out.
	if err := dev.SavePNG(*out); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote %s (%d frame(s) shown)\n", *out, len(dev.Frames()))
}

// drawAndShow is deliberately written against the interface, not the mock. It
// is the part you would keep when moving to real hardware.
func drawAndShow(dev epaper.Device) error {
	fonts := testcard.Fonts()

	c := render.NewCanvasFor(dev)
	c.Fill(epaper.White)

	w := c.Bounds().Dx()
	c.Rect(image.Rect(0, 0, w, 40), epaper.Red)
	c.TextFitted(image.Rect(8, 6, w-8, 34), "OFFLINE RENDER", fonts, epaper.White)

	// Ordered dither is how a four-ink panel gets more than four tones. The
	// bench proved this hardware resolves it cleanly at 1px.
	for i := range 10 {
		ratio := float64(i) / 9
		c.Dither(image.Rect(8+i*38, 56, 8+i*38+34, 110), epaper.Black, epaper.White, ratio)
	}

	body := "Everything above was drawn with no panel attached. " +
		"The mock records each frame, validates it exactly as the driver " +
		"would, and hands it back for inspection."
	c.TextWrapped(image.Rect(8, 124, w-8, 260), body, mustFace(fonts, 13), epaper.Black)

	c.Checker(image.Rect(8, 272, w-8, 292), epaper.Yellow, epaper.Red, 1)

	if err := c.Err(); err != nil {
		return err
	}
	return dev.Show(context.Background(), c.Image())
}

func mustFace(fonts render.FontFamily, size int) font.Face {
	f, err := fonts(size)
	if err != nil {
		log.Fatal(err)
	}
	return f
}

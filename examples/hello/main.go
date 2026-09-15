// Command hello is the smallest useful epaper program.
//
// It finds the panel, draws a line of text, and shows it. A full refresh takes
// about 20 seconds.
//
//	go run ./examples/hello
//
// Nothing here mentions SPI, GPIO, chip select, bit packing or refresh
// sequencing. That is the whole point of the library.
package main

import (
	"context"
	"image"
	"log"
	"time"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/inky"
	"github.com/sweeney/epaper/render"
	"github.com/sweeney/epaper/testcard"
)

func main() {
	dev, err := inky.Open()
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = dev.Close() }()

	// Bounds and palette both come from the device, so the image can never
	// disagree with the panel it is going to.
	c := render.NewCanvasFor(dev)
	c.Fill(epaper.White)

	// A red header bar with the text reversed out of it.
	header := image.Rect(0, 0, c.Bounds().Dx(), 48)
	c.Rect(header, epaper.Red)

	// testcard.Fonts needs no font file. Supply your own face if you would
	// rather — but read render.FontFamily first, because below about 17px a
	// scaled outline font cannot render legibly on a panel with no greys.
	fonts := testcard.Fonts()

	c.TextFitted(header.Inset(6), "HELLO", fonts, epaper.White)
	c.TextFitted(image.Rect(8, 60, 392, 88), time.Now().Format("Mon 2 Jan, 15:04"), fonts, epaper.Black)

	// One error check for all the drawing above. The first failure is
	// recorded and later calls become no-ops, so nothing is hidden.
	if err := c.Err(); err != nil {
		log.Fatal(err)
	}

	// A refresh is far too long to be uncancellable, so Show takes a context.
	// Cancelling abandons the wait, not the refresh: the panel finishes
	// redrawing regardless.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if err := dev.Show(ctx, c.Image()); err != nil {
		log.Fatal(err)
	}
}

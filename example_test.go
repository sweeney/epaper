package epaper_test

import (
	"context"
	"fmt"
	"image"
	"log"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/mock"
	"github.com/sweeney/epaper/render"
)

// The whole library, end to end. A real program would call inky.Open()
// instead of mock.New; nothing else changes, which is the point.
func Example() {
	dev := mock.New(400, 300, fourInk) // stands in for inky.Open()
	defer dev.Close()

	c := render.NewCanvasFor(dev) // bounds and palette both come from the device
	c.Fill(epaper.White)
	c.Rect(image.Rect(0, 0, 400, 30), epaper.Red)
	if err := c.Err(); err != nil {
		log.Fatal(err)
	}

	if err := dev.Show(context.Background(), c.Image()); err != nil {
		log.Fatal(err)
	}

	fmt.Println("frames shown:", len(dev.Frames()))
	// Output: frames shown: 1
}

// An ink the panel does not have is reported, never quietly substituted.
func ExamplePalette_Index() {
	if _, ok := fourInk.Index(epaper.Red); ok {
		fmt.Println("this panel has red")
	}
	if _, ok := fourInk.Index(epaper.Green); !ok {
		fmt.Println("this panel has no green")
	}
	// Output:
	// this panel has red
	// this panel has no green
}

// NearestTo is the explicit opt-in to a lossy substitution.
func ExamplePalette_NearestTo() {
	fmt.Println("green becomes", fourInk.NearestTo(epaper.Green))
	fmt.Println("red stays", fourInk.NearestTo(epaper.Red))
	// Output:
	// green becomes yellow
	// red stays red
}

// Pack converts an image to the panel's two-bits-per-pixel wire format.
func ExamplePack() {
	img := image.NewPaletted(image.Rect(0, 0, 4, 1), fourInk.Colors())
	img.Pix = []uint8{0, 1, 2, 3} // black, white, yellow, red

	frame, err := epaper.Pack(img)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d pixels -> %d byte: %08b\n", 4, len(frame), frame[0])
	// Output: 4 pixels -> 1 byte: 00011011
}

package testcard_test

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"log"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/mock"
	"github.com/sweeney/epaper/render"
	"github.com/sweeney/epaper/testcard"
)

var panel = epaper.Palette{
	{Ink: epaper.Black, RGB: color.RGBA{0, 0, 0, 255}},
	{Ink: epaper.White, RGB: color.RGBA{255, 255, 255, 255}},
	{Ink: epaper.Yellow, RGB: color.RGBA{235, 205, 40, 255}},
	{Ink: epaper.Red, RGB: color.RGBA{190, 45, 40, 255}},
}

// Proving a panel is one call. If what appears looks like the card, then the
// wiring, the transport, the command sequence, all four inks, the dithering
// and the text rendering are all working.
func ExampleShow() {
	// A real program opens the panel with inky.Open(); the mock stands in
	// here so the example runs anywhere.
	dev := mock.New(400, 300, panel)
	defer dev.Close()

	if err := testcard.Show(context.Background(), dev, "bench check"); err != nil {
		log.Fatal(err)
	}

	fmt.Println("frames shown:", len(dev.Frames()))
	// Output: frames shown: 1
}

// Options.Lines is where to put whatever identifies the run, so a photograph
// of the panel records what produced it.
func ExampleDraw() {
	c := render.NewCanvas(image.Rect(0, 0, 400, 300), panel)

	testcard.Draw(c, testcard.Options{
		Title: "GREENHOUSE",
		Lines: []string{"build 41af2c", "2026-09-15 04:20"},
	})

	if err := c.Err(); err != nil {
		log.Fatal(err)
	}
	fmt.Println("drawn:", c.Image().Bounds())
	// Output: drawn: (0,0)-(400,300)
}

// The conformance pattern contains no text, which is what makes it comparable
// byte for byte against the vendor library's output.
func ExampleDrawConformance() {
	c := render.NewCanvas(image.Rect(0, 0, 400, 300), panel)
	testcard.DrawConformance(c)

	frame, err := epaper.Pack(c.Image())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("packed bytes:", len(frame))
	// Output: packed bytes: 30000
}

// Fonts needs no font file: it hands out bitmap faces, which are the only
// thing that renders small text legibly on a panel with no greys.
func ExampleFonts() {
	fonts := testcard.Fonts()

	small, err := fonts(10)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("10px line height:", render.LineHeight(small))

	// Large sizes are the same face integer-scaled, so a heading is exactly
	// as crisp as the body text. Sizes come in steps, and what comes back is
	// never taller than what was asked for.
	big, err := fonts(48)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("48px gives a face of height:", render.LineHeight(big))
	// Output:
	// 10px line height: 13
	// 48px gives a face of height: 34
}

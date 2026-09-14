package render_test

import (
	"fmt"
	"image"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/render"
)

// Draw calls return nothing; the first failure is recorded and checked once at
// the end, like bufio.Writer. Drawing code that checks an error on every line
// does not get written.
func ExampleCanvas_Err() {
	c := render.NewCanvas(image.Rect(0, 0, 100, 50), fourInk)

	c.Fill(epaper.White)
	c.Rect(image.Rect(0, 0, 100, 10), epaper.Green) // not on a four-ink panel
	c.Fill(epaper.Black)                            // a no-op: the error is sticky

	fmt.Println(c.Err())
	// Output: render: ink green is not on this panel: render: ink not available on this palette
}

// TextFitted shrinks until the string fits, and reports the size it used.
// On e-ink there is no scrollbar: text that does not fit simply is not there.
func ExampleCanvas_TextFitted() {
	c := render.NewCanvas(image.Rect(0, 0, 200, 40), fourInk)
	c.Fill(epaper.White)

	wide := c.TextFitted(image.Rect(0, 0, 200, 30), "RED/YELLOW", testFamily, epaper.Black)
	narrow := c.TextFitted(image.Rect(0, 0, 80, 30), "RED/YELLOW", testFamily, epaper.Black)

	fmt.Println("fits a 200px box at:", wide > narrow)
	fmt.Println("no error:", c.Err() == nil)
	// Output:
	// fits a 200px box at: true
	// no error: true
}

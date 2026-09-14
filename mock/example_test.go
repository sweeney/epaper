package mock_test

import (
	"context"
	"fmt"
	"image"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/mock"
	"github.com/sweeney/epaper/render"
)

// Build and test a whole display program with no hardware in the room.
func Example() {
	dev := mock.New(400, 300, fourInk)
	defer dev.Close()

	c := render.NewCanvasFor(dev)
	c.Fill(epaper.White)
	c.Rect(image.Rect(10, 10, 110, 40), epaper.Red)

	if err := dev.Show(context.Background(), c.Image()); err != nil {
		fmt.Println("show failed:", err)
		return
	}

	// Inspect exactly what would have gone to the panel.
	last := dev.Last()
	fmt.Println("pixel (50,20) is red:", last.ColorIndexAt(50, 20) == 3)
	fmt.Println("pixel (200,200) is white:", last.ColorIndexAt(200, 200) == 1)
	// Output:
	// pixel (50,20) is red: true
	// pixel (200,200) is white: true
}

// FailNextShow exercises a consumer's error path, which on a display nobody
// watches is the code most likely to be wrong.
func ExampleDevice_FailNextShow() {
	dev := mock.New(400, 300, fourInk)
	defer dev.Close()

	dev.FailNextShow(fmt.Errorf("panel unplugged"))

	err := dev.Show(context.Background(), dev.NewImage())
	fmt.Println("first show:", err)
	fmt.Println("second show:", dev.Show(context.Background(), dev.NewImage()))
	// Output:
	// first show: panel unplugged
	// second show: <nil>
}

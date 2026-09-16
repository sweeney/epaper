// Command dashboard draws a realistic status panel.
//
//	go run ./examples/dashboard -png out.png    # no hardware needed
//	go run ./examples/dashboard                 # on the Pi, ~20 s
//
// This is the shape most e-ink projects end up in: some numbers, a couple of
// bars, a timestamp, refreshed every few minutes. It shows the things that
// turn out to matter once you build one for real —
//
//   - measuring text so values can be right-aligned
//   - using an accent ink to mean something, not to decorate
//   - dither as a gauge fill, because four inks give you no other tone
//   - checking the layout fits, rather than discovering it did not 25
//     seconds later in another room
//
// The drawing is a pure function from data to an image, which is what makes
// it testable. See ../consumertest for that half.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"image"
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

// Reading is one line on the panel.
type Reading struct {
	Label string
	Value string
	// Alert draws the value in the accent ink. Use it to mean something:
	// on a display nobody is watching, a colour that is merely decorative
	// trains the eye to ignore it.
	Alert bool
}

// Status is everything the panel shows. Keeping it in one struct is what lets
// the drawing be a pure function, and therefore testable.
type Status struct {
	Title    string
	Readings []Reading
	Gauge    struct {
		Label string
		Ratio float64 // 0..1
	}
	Footer string
}

func main() {
	pngPath := flag.String("png", "", "render to this file instead of the panel")
	width := flag.Int("width", 400, "with -png: panel width to render at")
	height := flag.Int("height", 300, "with -png: panel height to render at")
	flag.Parse()

	status := sample()

	if *pngPath != "" {
		img, err := renderTo(*width, *height, panelPalette, status)
		if err != nil {
			log.Fatal(err)
		}
		if err := writePNG(*pngPath, img); err != nil {
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

	c, err := drawWhatFits(dev.Bounds(), dev.Palette(), status)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	started := time.Now()
	if err := dev.Show(ctx, c.Image()); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("refreshed in %s\n", time.Since(started).Round(time.Millisecond))
}

// Draw paints the status onto a canvas. It is exported and takes only a canvas
// and data, so a test can call it with a mock-sized canvas and assert on the
// pixels — no hardware, no 25-second wait.
func Draw(c *render.Canvas, s Status) error {
	fonts := testcard.Fonts()
	b := c.Bounds()
	w := b.Dx()

	c.Fill(epaper.White)

	// Row and label faces first: the chrome is sized from what the text
	// actually measures, not from constants. A 400x300 wHAT and a 250x122
	// pHAT differ by a factor of two and a half in height, and a fixed 54px
	// header leaves a pHAT nothing at all to put readings in.
	row, err := fonts(16)
	if err != nil {
		return err
	}
	small, err := fonts(13)
	if err != nil {
		return err
	}
	lh := render.LineHeight(row)
	smallLH := render.LineHeight(small)

	// --- header
	//
	// The box is deliberately tall enough, where there is room, for the font
	// family to reach for a scaled bitmap face rather than its 16px one. On a
	// 1-bit panel an integer-scaled bitmap glyph stays perfectly crisp, so a
	// big heading costs nothing in quality — see render.ScaleFace. On a short
	// panel it falls back to just enough for one line.
	headerH := max(b.Dy()*54/300, lh+6)
	pad := max(b.Dy()*7/300, 2)
	c.Rect(image.Rect(0, 0, w, headerH), epaper.Red)
	c.TextFitted(image.Rect(10, pad, w-10, headerH-pad), s.Title, fonts, epaper.White)

	// The bottom block: gauge label, bar, footer. Measured rather than
	// guessed, so it reserves exactly what it is going to use.
	barH := max(b.Dy()*18/300, 8)
	bottomH := smallLH + 2 + barH + 6 + smallLH

	// --- readings, one per row, value right-aligned
	y := headerH + max(b.Dy()*12/300, 4)
	for _, r := range s.Readings {
		if y+lh > b.Max.Y-bottomH {
			// Out of room. Saying so beats drawing over the footer, and
			// beats silently dropping the row.
			return fmt.Errorf("dashboard: %d readings do not fit; ran out at %q", len(s.Readings), r.Label)
		}

		c.Text(image.Pt(12, y), r.Label, row, epaper.Black)

		// Right-align by measuring. Guessing a column position is how a
		// value ends up half off the edge with nothing to show for it.
		ink := epaper.Black
		if r.Alert {
			ink = epaper.Red
		}
		c.Text(image.Pt(w-12-render.MeasureText(r.Value, row), y), r.Value, row, ink)

		// A hairline rule, dithered so it reads as a separator rather than
		// competing with the text.
		c.Dither(image.Rect(12, y+lh+2, w-12, y+lh+3), epaper.Black, epaper.White, 0.5)
		y += lh + 8
	}

	// --- gauge
	gy := b.Max.Y - bottomH
	c.Text(image.Pt(12, gy), s.Gauge.Label, small, epaper.Black)

	bar := image.Rect(12, gy+smallLH+2, w-12, gy+smallLH+2+barH)
	c.StrokeRect(bar, epaper.Black)

	// Fill the bar with a solid ink up to the ratio, and a light dither
	// beyond it, so the empty part still reads as part of the gauge.
	inner := bar.Inset(2)
	split := inner.Min.X + int(float64(inner.Dx())*clamp(s.Gauge.Ratio))
	c.Rect(image.Rect(inner.Min.X, inner.Min.Y, split, inner.Max.Y), epaper.Yellow)
	c.Dither(image.Rect(split, inner.Min.Y, inner.Max.X, inner.Max.Y), epaper.Black, epaper.White, 0.15)

	// --- footer
	c.Text(image.Pt(12, b.Max.Y-smallLH), s.Footer, small, epaper.Black)

	return c.Err()
}

// drawWhatFits draws as much of the status as the panel will hold, dropping
// the least important readings until it does.
//
// This is the pattern a real consumer wants, and it is only possible because
// Draw REPORTS that it ran out of room rather than silently overdrawing. The
// library's whole posture is that a layout which does not fit is an error, not
// a picture; this is what you do with that error. Drawing is microseconds, so
// trying again costs nothing next to the ~18 s refresh.
//
// It matters here because the two panels this library drives differ by a
// factor of two and a half in height: the same four readings that fit a
// 400x300 wHAT cannot fit a 250x122 pHAT, and no fixed layout serves both.
func drawWhatFits(bounds image.Rectangle, palette epaper.Palette, s Status) (*render.Canvas, error) {
	for {
		// A canvas records the first failure and turns every later call into
		// a no-op, so each attempt needs a fresh one.
		c := render.NewCanvas(bounds, palette)
		err := Draw(c, s)
		if err == nil {
			return c, nil
		}
		if len(s.Readings) == 0 {
			return nil, err // not the readings: something else does not fit
		}
		s.Readings = s.Readings[:len(s.Readings)-1]
	}
}

func clamp(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	}
	return v
}

// renderTo draws onto a mock of the given shape, which is all the "no
// hardware" path needs.
//
// It goes through drawWhatFits for the same reason the panel path does: an
// offline preview that used a different layout rule from the real one would be
// a preview of nothing. That is the whole point of rendering against a mock of
// the device rather than against a bare canvas.
func renderTo(w, h int, p epaper.Palette, s Status) (image.Image, error) {
	dev := mock.New(w, h, p)
	defer func() { _ = dev.Close() }()

	c, err := drawWhatFits(dev.Bounds(), dev.Palette(), s)
	if err != nil {
		return nil, err
	}
	if err := dev.Show(context.Background(), c.Image()); err != nil {
		return nil, err
	}
	return dev.Last(), nil
}

func sample() Status {
	s := Status{
		Title: "HOUSE",
		Readings: []Reading{
			{Label: "Living room", Value: "21.4 C"},
			{Label: "Outside", Value: "3.1 C"},
			{Label: "Power draw", Value: "412 W"},
			{Label: "Boiler", Value: "FAULT", Alert: true},
		},
		Footer: "updated " + time.Now().Format("Mon 2 Jan 15:04"),
	}
	s.Gauge.Label = "Battery"
	s.Gauge.Ratio = 0.62
	return s
}

var panelPalette = epaper.Palette{
	{Ink: epaper.Black, RGB: rgb(0, 0, 0)},
	{Ink: epaper.White, RGB: rgb(255, 255, 255)},
	{Ink: epaper.Yellow, RGB: rgb(235, 205, 40)},
	{Ink: epaper.Red, RGB: rgb(190, 45, 40)},
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		return errors.Join(err, f.Close())
	}
	return f.Close()
}

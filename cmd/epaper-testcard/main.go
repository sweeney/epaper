// Command epaper-testcard draws a test pattern, either on a real panel or to
// a PNG file.
//
// It is both a demo of the library and the hardware acceptance test: if the
// card looks right on the glass, the whole stack works.
//
//	epaper-testcard                      # draw the test card on the panel
//	epaper-testcard -pattern conformance  # draw the conformance pattern
//	epaper-testcard -png card.png         # render to a file, no hardware needed
//	epaper-testcard -png c.png -size 250x122   # ...at another panel's geometry
//	epaper-testcard -png out/card.png -size all # ...at every supported geometry
//	epaper-testcard -list                 # what this library supports
//
// The -png mode needs no Pi and no panel, which is the point: a layout can be
// checked in milliseconds rather than the 20 seconds a refresh costs.
//
// -size only applies to -png. On real hardware the panel's EEPROM decides the
// geometry, and overriding it would draw something the panel cannot show.
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
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/driver/jd79668"
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
		size    = flag.String("size", "", "with -png: the geometry to render at — WxH, a panel model, or \"all\" (default: every supported panel)")
		list    = flag.Bool("list", false, "list the panels this library supports, and exit")
		pattern = flag.String("pattern", "testcard", "which pattern: testcard, orientation or conformance")
		note    = flag.String("note", "", "extra line of text on the card")
		timeout = flag.Duration("timeout", 2*time.Minute, "how long to wait for the refresh")
		vendor  = flag.Bool("vendor-timing", false,
			"restore the reference implementation's 300ms per-command delay (~4.9s slower)")
	)
	flag.Parse()

	if *list {
		return listPanels()
	}
	if !validPattern(*pattern) {
		return fmt.Errorf("unknown pattern %q: want testcard, orientation or conformance", *pattern)
	}
	if *size != "" && *pngPath == "" {
		return errors.New("-size only applies to -png: on a real panel the EEPROM decides the geometry")
	}

	// Off-panel: render against a stand-in of the real device, so the image
	// is identical to what the panel would be sent.
	if *pngPath != "" {
		return renderPNGs(*pngPath, *size, *pattern, *note)
	}

	dev, err := inky.OpenWith(inky.Options{CommandDelay: commandDelay(*vendor)})
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

	fmt.Printf("drawing %s — a full refresh takes about 20 seconds...\n", *pattern)
	started := time.Now()
	if err := dev.Show(ctx, img); err != nil {
		return err
	}
	fmt.Printf("done in %s\n", time.Since(started).Round(time.Millisecond))
	return nil
}

// commandDelay turns -vendor-timing into a delay. Zero is the driver's
// default and means none.
func commandDelay(vendor bool) time.Duration {
	if vendor {
		return jd79668.VendorCommandDelay
	}
	return 0
}

func validPattern(p string) bool {
	switch p {
	case "testcard", "orientation", "conformance":
		return true
	}
	return false
}

// listPanels prints what this library can drive. It needs no hardware: the
// list comes from the drivers that are compiled in.
func listPanels() error {
	fmt.Printf("%-10s  %-30s  %-10s  %s\n", "GEOMETRY", "MODEL", "CONTROLLER", "EEPROM VARIANT")
	for _, p := range inky.SupportedPanels() {
		fmt.Printf("%-10s  %-30s  %-10s  %d\n",
			fmt.Sprintf("%dx%d", p.Width, p.Height), p.Model, p.Controller, p.DisplayVariant)
	}
	return nil
}

// renderPNGs writes the pattern at one geometry, or at every supported one.
//
// Rendering every panel by default is deliberate: the card is laid out from
// the panel's own size, so "it looks right" is a claim about one geometry
// until it has been looked at on the others. That is exactly how the card came
// to be scrambled at 250x122 while its 400x300 golden passed.
func renderPNGs(path, size, pattern, note string) error {
	panels, err := panelsFor(size)
	if err != nil {
		return err
	}
	for _, p := range panels {
		img, err := draw(image.Rect(0, 0, p.Width, p.Height), paletteForPNG(), pattern, p.Model, note)
		if err != nil {
			return fmt.Errorf("%dx%d: %w", p.Width, p.Height, err)
		}
		out := path
		if len(panels) > 1 {
			out = suffixed(path, fmt.Sprintf("-%dx%d", p.Width, p.Height))
		}
		if err := writePNG(out, img); err != nil {
			return err
		}
	}
	return nil
}

// panelsFor resolves -size to the geometries to render.
//
// An explicit WxH is allowed to be a panel nobody sells: the point of the flag
// is to see what the layout does at a size, and refusing unknown ones would
// make it useless for exactly the case it is for — checking a panel before
// writing its driver.
func panelsFor(size string) ([]inky.Panel, error) {
	if size == "" || size == "all" {
		return inky.SupportedPanels(), nil
	}
	if w, h, ok := parseSize(size); ok {
		if w <= 0 || h <= 0 {
			return nil, fmt.Errorf("size %q: both dimensions must be positive", size)
		}
		return []inky.Panel{{Model: fmt.Sprintf("%dx%d", w, h), Width: w, Height: h}}, nil
	}
	// Not a geometry, so try it as a model name — a substring match, so
	// "pHAT" is enough and nobody has to type the brackets.
	var matched []inky.Panel
	for _, p := range inky.SupportedPanels() {
		if strings.Contains(strings.ToLower(p.Model), strings.ToLower(size)) ||
			strings.EqualFold(p.Controller, size) {
			matched = append(matched, p)
		}
	}
	if len(matched) == 0 {
		return nil, fmt.Errorf("size %q is neither a WxH geometry nor a panel this library supports "+
			"(try -list)", size)
	}
	return matched, nil
}

func parseSize(s string) (w, h int, ok bool) {
	x := strings.IndexAny(s, "xX")
	if x <= 0 || x == len(s)-1 {
		return 0, 0, false
	}
	w, err := strconv.Atoi(s[:x])
	if err != nil {
		return 0, 0, false
	}
	h, err = strconv.Atoi(s[x+1:])
	if err != nil {
		return 0, 0, false
	}
	return w, h, true
}

// suffixed inserts a suffix before a path's extension: card.png -> card-250x122.png.
func suffixed(path, suffix string) string {
	ext := filepath.Ext(path)
	return strings.TrimSuffix(path, ext) + suffix + ext
}

func draw(bounds image.Rectangle, palette epaper.Palette, pattern, model, note string) (*image.Paletted, error) {
	c := render.NewCanvas(bounds, palette)
	switch pattern {
	case "conformance":
		testcard.DrawConformance(c)
	case "orientation":
		testcard.DrawOrientation(c)
	default:
		testcard.Draw(c, testcard.Options{Lines: []string{model, note}})
	}
	if err := c.Err(); err != nil {
		return nil, err
	}
	return c.Image(), nil
}

func writePNG(path string, img image.Image) error {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
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

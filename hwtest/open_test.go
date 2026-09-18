//go:build hardware

package hwtest

import (
	"image"
	"testing"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/inky"
)

// panel describes what is expected of whichever board is plugged in. The
// hardware suite runs against two of them now, so nothing here may assume a
// size — an assertion of 400x300 passes on a wHAT and fails on a pHAT for a
// reason that has nothing to do with the code under test.
//
// The EEPROM is the authority: it is what Open dispatches on, so checking the
// device against it is checking that the dispatch did what the board asked
// for.
func expectedPanel(t *testing.T) *inky.EEPROM {
	t.Helper()
	info, err := inky.Identify(inky.DefaultI2CPath)
	if err != nil {
		t.Fatalf("inky.Identify(): %v", err)
	}
	return info
}

// M7's acceptance test: Open returns a working device on the Pi.
//
// It does not draw. Opening claims the GPIO lines and the SPI bus but leaves
// the panel alone, so this costs nothing and can run as often as you like.
func TestOpen(t *testing.T) {
	dev, err := inky.Open()
	if err != nil {
		t.Fatalf("inky.Open(): %v", err)
	}
	defer dev.Close()

	info := expectedPanel(t)
	t.Logf("model: %q", dev.Model())
	t.Logf("bounds: %v", dev.Bounds())
	t.Logf("palette: %d inks", len(dev.Palette()))
	t.Logf("eeprom: %dx%d %s, pcb v%s, display variant %d, written %s",
		info.Width, info.Height, info.Colour, info.PCBRevision(), info.DisplayVariant, info.WriteTime)

	// The picture's geometry and inks come from the supported-panel entry
	// for the board's variant, not straight from the EEPROM: the Impression's
	// EEPROM records its controller's portrait 400x600, and the picture is
	// 600x400. So look the board up, and check the device against that.
	var panel *inky.Panel
	for _, p := range inky.SupportedPanels() {
		if p.DisplayVariant == info.DisplayVariant {
			panel = &p
		}
	}
	if panel == nil {
		t.Fatalf("display variant %d opened but is not in SupportedPanels", info.DisplayVariant)
	}

	if got, want := dev.Bounds(), image.Rect(0, 0, panel.Width, panel.Height); got != want {
		t.Errorf("Bounds() = %v, want %v", got, want)
	}
	if dev.Bounds().Dx() < dev.Bounds().Dy() {
		t.Errorf("Bounds() = %v is portrait; every supported panel presents landscape", dev.Bounds())
	}
	if dev.Model() != info.Model {
		t.Errorf("Model() = %q, want the EEPROM name %q", dev.Model(), info.Model)
	}

	// Every ink the board's colour string promises, and no others. "red/yellow"
	// means both at once — the handover notes said three inks, and were wrong.
	want := map[string][]epaper.Ink{
		"red/yellow": {epaper.Black, epaper.White, epaper.Yellow, epaper.Red},
		"spectra6":   {epaper.Black, epaper.White, epaper.Yellow, epaper.Red, epaper.Blue, epaper.Green},
	}[info.Colour]
	if want == nil {
		t.Fatalf("no expectation for colour %q; add one", info.Colour)
	}
	if got := len(dev.Palette()); got != len(want) {
		t.Errorf("palette has %d inks, want %d for %q", got, len(want), info.Colour)
	}
	for _, ink := range want {
		if !dev.Palette().Has(ink) {
			t.Errorf("palette has no %s", ink)
		}
	}

	// The image it hands out must be one it will accept back.
	img := dev.NewImage()
	if img.Bounds() != dev.Bounds() {
		t.Errorf("NewImage() bounds = %v, want %v", img.Bounds(), dev.Bounds())
	}
}

// Opening twice in a row must work: the first Close has to actually release
// the GPIO lines, or the second Open fails with a busy line.
func TestOpenReleasesItsLines(t *testing.T) {
	for i := range 2 {
		dev, err := inky.Open()
		if err != nil {
			t.Fatalf("Open() attempt %d: %v", i+1, err)
		}
		if err := dev.Close(); err != nil {
			t.Fatalf("Close() attempt %d: %v", i+1, err)
		}
	}
}

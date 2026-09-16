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

	if got, want := dev.Bounds(), image.Rect(0, 0, info.Width, info.Height); got != want {
		t.Errorf("Bounds() = %v, want %v — the EEPROM's geometry did not reach the driver", got, want)
	}
	if dev.Model() != info.Model {
		t.Errorf("Model() = %q, want the EEPROM name %q", dev.Model(), info.Model)
	}

	// All four inks, simultaneously. The handover notes said three.
	for _, ink := range []epaper.Ink{epaper.Black, epaper.White, epaper.Yellow, epaper.Red} {
		if !dev.Palette().Has(ink) {
			t.Errorf("palette has no %s", ink)
		}
	}
	if dev.Palette().Has(epaper.Green) {
		t.Error("palette claims to have green")
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

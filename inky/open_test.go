package inky_test

import (
	"errors"
	"testing"

	"github.com/sweeney/epaper/inky"
)

// Pimoroni's assignment, from inky_jd79668.py. These are wiring facts; if one
// changes, the panel goes dark for a reason nothing else will explain.
func TestDefaultPins(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  int
		want int
	}{
		{"Reset", inky.DefaultPins.Reset, 27},
		{"Busy", inky.DefaultPins.Busy, 17},
		{"DataCommand", inky.DefaultPins.DataCommand, 22},
		{"ChipSelect", inky.DefaultPins.ChipSelect, 8},
	} {
		if tc.got != tc.want {
			t.Errorf("DefaultPins.%s = %d, want GPIO %d", tc.name, tc.got, tc.want)
		}
	}
}

func TestDefaultPaths(t *testing.T) {
	if inky.DefaultSPIPath != "/dev/spidev0.0" {
		t.Errorf("DefaultSPIPath = %q", inky.DefaultSPIPath)
	}
	if inky.DefaultI2CPath != "/dev/i2c-1" {
		t.Errorf("DefaultI2CPath = %q", inky.DefaultI2CPath)
	}
	if inky.DefaultSPISpeedHz != 1_000_000 {
		t.Errorf("DefaultSPISpeedHz = %d, want 1 MHz", inky.DefaultSPISpeedHz)
	}
}

// Opening cannot work without the hardware, but it must FAIL rather than hang
// or panic, and the error has to be one a caller can act on.
func TestOpenWithoutHardwareFailsCleanly(t *testing.T) {
	_, err := inky.OpenWith(inky.Options{
		I2CPath: "/dev/does-not-exist",
		SPIPath: "/dev/does-not-exist",
	})
	if err == nil {
		t.Fatal("OpenWith() = nil error with no hardware present")
	}
	if !contains(err.Error(), "inky:") {
		t.Errorf("error %q is not attributed to this package", err)
	}
}

func TestIdentifyWithoutHardwareFailsCleanly(t *testing.T) {
	if _, err := inky.Identify("/dev/does-not-exist"); err == nil {
		t.Fatal("Identify() = nil error with no hardware present")
	}
}

// The chip-select error must carry the fix, not just the symptom. Without
// dtoverlay=spi0-0cs the kernel owns GPIO 8, and the failure looks like a busy
// line rather than a missing device — twenty minutes of debugging for anyone
// who has not met it before.
func TestChipSelectErrorCarriesTheFix(t *testing.T) {
	err := inky.ChipSelectAdvice(inky.DefaultPins)
	if !errors.Is(err, inky.ErrChipSelectBusy) {
		t.Fatalf("error = %v, want ErrChipSelectBusy", err)
	}
	for _, want := range []string{"dtoverlay=spi0-0cs", "config.txt", "reboot", "8"} {
		if !contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

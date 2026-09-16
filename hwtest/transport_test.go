//go:build hardware

package hwtest

import (
	"errors"
	"testing"

	"github.com/sweeney/epaper/inky"
	"github.com/sweeney/epaper/internal/gpiocdev"
	"github.com/sweeney/epaper/internal/i2c"
	"github.com/sweeney/epaper/internal/spidev"
)

// Pimoroni's pin assignments. From reference/vendor-inky/inky_jd79668.py.
const (
	pinReset = 27
	pinBusy  = 17
	pinDC    = 22
	pinCS    = 8
)

// M5's acceptance test: read our EEPROM and get the right struct back.
func TestI2CReadsTheEEPROM(t *testing.T) {
	bus, err := i2c.Open("/dev/i2c-1")
	if err != nil {
		t.Fatalf("opening I2C bus (is the user in the i2c group?): %v", err)
	}
	defer bus.Close()

	raw, err := bus.ReadReg16(0x50, 0x0000, 29)
	if err != nil {
		t.Fatalf("reading the EEPROM: %v", err)
	}
	t.Logf("EEPROM raw: % 02X", raw)

	got, err := inky.ParseEEPROM(raw)
	if err != nil {
		t.Fatalf("parsing the EEPROM: %v", err)
	}
	t.Logf("EEPROM: %dx%d %s pcb %s %q written %s",
		got.Width, got.Height, got.Colour, got.PCBRevision(), got.Model, got.WriteTime)

	// Deliberately not asserting a size or a model name: this suite runs
	// against more than one board, and pinning either here would fail on the
	// other for a reason that has nothing to do with I2C. What this test is
	// for is that the combined write-then-read returns a record at all rather
	// than the padding a byte-mode SMBus read gives back — so the assertions
	// are the ones that separate a real record from garbage.
	//
	// The per-board values are pinned where they belong, against captured
	// fixtures, in inky.TestParseRealPHat and inky.TestParseEEPROMRealBoard.
	if got.Width <= 0 || got.Height <= 0 {
		t.Errorf("geometry = %dx%d, which is not a real panel", got.Width, got.Height)
	}
	if got.Model == "" {
		t.Error("Model is empty; the display variant did not resolve")
	}
	if got.Colour != "red/yellow" {
		t.Errorf("Colour = %q, want %q — all four-ink Inky boards report this", got.Colour, "red/yellow")
	}

	// The board has to be one this library will actually drive, or every
	// other test in this suite is about to fail more confusingly.
	//
	// Closing it matters: Open claims the GPIO lines, and holding them here
	// would make TestGPIOClaimsThePanelLines fail with a busy line — which is
	// exactly the misleading failure ErrChipSelectBusy exists to explain, and
	// it is no more fun when this suite causes it.
	dev, err := inky.Open()
	if err != nil {
		t.Errorf("inky.Open() with this board attached: %v", err)
		return
	}
	if err := dev.Close(); err != nil {
		t.Errorf("Close(): %v", err)
	}
}

// The combined write-then-read is the whole point of the i2c package. If this
// ever starts returning 0xFF or 0x01 padding, something has reverted to a
// byte-mode SMBus read.
func TestI2CReadIsRepeatable(t *testing.T) {
	bus, err := i2c.Open("/dev/i2c-1")
	if err != nil {
		t.Fatalf("opening I2C bus: %v", err)
	}
	defer bus.Close()

	first, err := bus.ReadReg16(0x50, 0x0000, 29)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	for i := range 5 {
		again, err := bus.ReadReg16(0x50, 0x0000, 29)
		if err != nil {
			t.Fatalf("read %d: %v", i+2, err)
		}
		if string(again) != string(first) {
			t.Fatalf("read %d differs from the first:\n  % 02X\n  % 02X", i+2, first, again)
		}
	}
}

func TestGPIOFindChip(t *testing.T) {
	name, err := gpiocdev.FindChip()
	if err != nil {
		t.Fatalf("FindChip(): %v", err)
	}
	t.Logf("pin controller: %s", name)
}

// Claiming all four panel lines is what fails when dtoverlay=spi0-0cs is
// missing, and it fails specifically on chip-select.
func TestGPIOClaimsThePanelLines(t *testing.T) {
	lines, err := gpiocdev.Open(gpiocdev.Config{
		Consumer: "epaper-hwtest",
		Outputs:  map[int]int{pinCS: 1, pinDC: 0, pinReset: 1},
		Inputs:   []int{pinBusy},
	})
	if err != nil {
		if errors.Is(err, gpiocdev.ErrLineBusy) {
			t.Fatalf("a line is claimed by another driver — is dtoverlay=spi0-0cs set? %v", err)
		}
		t.Fatalf("claiming panel lines (is the user in the gpio group?): %v", err)
	}
	defer lines.Close()

	// BUSY has a pull-up, so with the panel idle it should read high. A low
	// reading here means the panel is mid-refresh, which it should not be.
	busy, err := lines.Get(pinBusy)
	if err != nil {
		t.Fatalf("reading BUSY: %v", err)
	}
	t.Logf("BUSY reads %d (1 = ready)", busy)

	// Toggling CS must not error. It is active low, so this leaves it released.
	for _, v := range []int{0, 1} {
		if err := lines.Set(pinCS, v); err != nil {
			t.Fatalf("setting CS to %d: %v", v, err)
		}
	}
}

func TestSPIOpens(t *testing.T) {
	dev, err := spidev.Open(spidev.Config{
		Path:    "/dev/spidev0.0",
		Mode:    spidev.Mode0,
		SpeedHz: 1_000_000,
	})
	if err != nil {
		t.Fatalf("opening SPI (is the user in the spi group? is SPI enabled?): %v", err)
	}
	defer dev.Close()

	t.Logf("SPI chunk size: %d bytes", dev.ChunkSize())
	if dev.ChunkSize() < 1 {
		t.Errorf("ChunkSize() = %d, want a positive size", dev.ChunkSize())
	}
}

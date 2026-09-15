package inky

import (
	"context"
	"errors"
	"image"
	"strings"
	"testing"
	"time"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/internal/gpiocdev"
	"github.com/sweeney/epaper/internal/spidev"
)

// realBoard is our wHAT's EEPROM record, decoded.
func realBoard() *EEPROM {
	return &EEPROM{
		Width: 400, Height: 300,
		Colour: "red/yellow", PCBVariant: 100,
		DisplayVariant: variantRedYellowWhatJD79668,
		Model:          "Red/Yellow wHAT (JD79668)",
		WriteTime:      "2025-08-20 15:51:55.5",
	}
}

// stubTransports swaps the hardware out for fakes and restores them after.
// It returns the configs each was opened with, which is what the wiring tests
// actually assert on.
type opened struct {
	gpio    *gpiocdev.Config
	spi     *spidev.Config
	i2cPath string
	bus     *fakeBus
}

func stubTransports(t *testing.T, info *EEPROM, infoErr error) *opened {
	t.Helper()
	o := &opened{bus: &fakeBus{}}

	origGPIO, origSPI, origIdentify := openGPIO, openSPI, identify
	t.Cleanup(func() { openGPIO, openSPI, identify = origGPIO, origSPI, origIdentify })

	identify = func(path string) (*EEPROM, error) {
		o.i2cPath = path
		return info, infoErr
	}
	openGPIO = func(cfg gpiocdev.Config) (gpioLines, error) {
		o.gpio = &cfg
		return o.bus, nil
	}
	openSPI = func(cfg spidev.Config) (spiWriter, error) {
		o.spi = &cfg
		return o.bus, nil
	}
	return o
}

// The pins are claimed at the right INITIAL levels. Every line on this board
// is active low, so claiming reset or chip select at 0 would assert them — a
// spurious pulse to the panel before the driver has said anything, which
// produces no error at all.
func TestOpenWithClaimsThePinsCorrectly(t *testing.T) {
	o := stubTransports(t, realBoard(), nil)

	dev, err := OpenWith(Options{})
	if err != nil {
		t.Fatalf("OpenWith(): %v", err)
	}
	defer dev.Close()

	if o.gpio == nil {
		t.Fatal("no GPIO lines were requested")
	}
	for _, tc := range []struct {
		name string
		pin  int
		want int
		why  string
	}{
		{"chip select", DefaultPins.ChipSelect, release, "active low; asserting it would select the panel"},
		{"reset", DefaultPins.Reset, release, "active low; asserting it would hold the panel in reset"},
		{"data/command", DefaultPins.DataCommand, levelCommand, "low means the next byte is a command"},
	} {
		got, ok := o.gpio.Outputs[tc.pin]
		if !ok {
			t.Errorf("%s (GPIO %d) was not requested as an output", tc.name, tc.pin)
			continue
		}
		if got != tc.want {
			t.Errorf("%s (GPIO %d) claimed at %d, want %d — %s", tc.name, tc.pin, got, tc.want, tc.why)
		}
	}

	// BUSY is an input, and must not have been claimed as an output.
	if len(o.gpio.Inputs) != 1 || o.gpio.Inputs[0] != DefaultPins.Busy {
		t.Errorf("inputs = %v, want just BUSY on GPIO %d", o.gpio.Inputs, DefaultPins.Busy)
	}
	if _, isOutput := o.gpio.Outputs[DefaultPins.Busy]; isOutput {
		t.Error("BUSY was claimed as an output; it would fight the panel")
	}
	if o.gpio.Consumer == "" {
		t.Error("no consumer name; the lines would be anonymous in gpioinfo")
	}
}

// The SPI bus is opened with the vendor's mode and clock.
func TestOpenWithConfiguresSPI(t *testing.T) {
	o := stubTransports(t, realBoard(), nil)

	dev, err := OpenWith(Options{})
	if err != nil {
		t.Fatalf("OpenWith(): %v", err)
	}
	defer dev.Close()

	if o.spi.Path != DefaultSPIPath {
		t.Errorf("SPI path = %q, want %q", o.spi.Path, DefaultSPIPath)
	}
	if o.spi.Mode != spidev.Mode0 {
		t.Errorf("SPI mode = %v, want mode 0", o.spi.Mode)
	}
	if o.spi.SpeedHz != DefaultSPISpeedHz {
		t.Errorf("SPI speed = %d, want %d", o.spi.SpeedHz, DefaultSPISpeedHz)
	}
	if o.i2cPath != DefaultI2CPath {
		t.Errorf("I2C path = %q, want %q", o.i2cPath, DefaultI2CPath)
	}
}

// The panel describes itself, and what it says has to reach the driver — the
// geometry ends up in the controller's resolution command, and getting it from
// a constant instead would quietly break a differently-sized board.
func TestOpenWithPassesTheEEPROMToTheDriver(t *testing.T) {
	stubTransports(t, realBoard(), nil)

	dev, err := OpenWith(Options{})
	if err != nil {
		t.Fatalf("OpenWith(): %v", err)
	}
	defer dev.Close()

	if got, want := dev.Bounds(), image.Rect(0, 0, 400, 300); got != want {
		t.Errorf("Bounds() = %v, want %v", got, want)
	}
	if got := dev.Model(); got != "Red/Yellow wHAT (JD79668)" {
		t.Errorf("Model() = %q, want the EEPROM's name", got)
	}
	for _, ink := range []epaper.Ink{epaper.Black, epaper.White, epaper.Yellow, epaper.Red} {
		if !dev.Palette().Has(ink) {
			t.Errorf("palette has no %s", ink)
		}
	}
}

func TestOpenWithHonoursOptions(t *testing.T) {
	o := stubTransports(t, realBoard(), nil)
	pins := Pins{Reset: 1, Busy: 2, DataCommand: 3, ChipSelect: 4}

	dev, err := OpenWith(Options{
		SPIPath: "/dev/spidev9.9", I2CPath: "/dev/i2c-9",
		Pins: &pins, SPISpeedHz: 2_000_000,
	})
	if err != nil {
		t.Fatalf("OpenWith(): %v", err)
	}
	defer dev.Close()

	if o.spi.Path != "/dev/spidev9.9" || o.spi.SpeedHz != 2_000_000 {
		t.Errorf("SPI opened as %+v, want the overrides", o.spi)
	}
	if o.i2cPath != "/dev/i2c-9" {
		t.Errorf("I2C path = %q, want the override", o.i2cPath)
	}
	if _, ok := o.gpio.Outputs[4]; !ok {
		t.Errorf("outputs = %v, want the overridden chip select on GPIO 4", o.gpio.Outputs)
	}
}

// A panel we have no driver for must be refused BEFORE any hardware is
// claimed. Claiming lines and then failing would leave them held until the
// process exits.
func TestOpenWithRejectsAnUnknownPanelBeforeClaimingAnything(t *testing.T) {
	info := realBoard()
	info.DisplayVariant = 17 // Black wHAT (SSD1683): real, but not ours
	info.Model = "Black wHAT (SSD1683)"
	o := stubTransports(t, info, nil)

	_, err := OpenWith(Options{})
	if !errors.Is(err, ErrUnsupportedPanel) {
		t.Fatalf("OpenWith() = %v, want ErrUnsupportedPanel", err)
	}
	if !strings.Contains(err.Error(), "Black wHAT (SSD1683)") {
		t.Errorf("error %q does not name the panel", err)
	}
	if o.gpio != nil || o.spi != nil {
		t.Error("hardware was claimed before the panel was rejected")
	}
}

func TestOpenWithPropagatesAnEEPROMFailure(t *testing.T) {
	boom := errors.New("no HAT")
	o := stubTransports(t, nil, boom)

	if _, err := OpenWith(Options{}); !errors.Is(err, boom) {
		t.Fatalf("OpenWith() = %v, want the EEPROM error", err)
	}
	if o.gpio != nil || o.spi != nil {
		t.Error("hardware was claimed despite the EEPROM read failing")
	}
}

// If SPI fails after the lines are claimed, the lines must be handed back.
// Leaking them means the next run fails with a busy line and a misleading
// suggestion to check the device-tree overlay.
func TestOpenWithReleasesLinesWhenSPIFails(t *testing.T) {
	stubTransports(t, realBoard(), nil)

	var released bool
	orig := openGPIO
	openGPIO = func(cfg gpiocdev.Config) (gpioLines, error) {
		return &closeSpy{onClose: func() { released = true }}, nil
	}
	defer func() { openGPIO = orig }()

	boom := errors.New("no such device")
	openSPI = func(spidev.Config) (spiWriter, error) { return nil, boom }

	if _, err := OpenWith(Options{}); !errors.Is(err, boom) {
		t.Fatalf("OpenWith() = %v, want the SPI error", err)
	}
	if !released {
		t.Error("the GPIO lines were not released after SPI failed")
	}
}

// A busy line has to arrive as the chip-select advice, not as a raw errno.
func TestOpenWithTurnsABusyLineIntoAdvice(t *testing.T) {
	stubTransports(t, realBoard(), nil)
	openGPIO = func(gpiocdev.Config) (gpioLines, error) {
		return nil, gpiocdev.ErrLineBusy
	}

	_, err := OpenWith(Options{})
	if !errors.Is(err, ErrChipSelectBusy) {
		t.Fatalf("OpenWith() = %v, want ErrChipSelectBusy", err)
	}
	if !strings.Contains(err.Error(), "dtoverlay=spi0-0cs") {
		t.Errorf("error %q does not carry the fix", err)
	}
}

// closeSpy is a gpioLines that only records being closed.
type closeSpy struct {
	fakeBus
	onClose func()
}

func (c *closeSpy) Close() error {
	if c.onClose != nil {
		c.onClose()
	}
	return nil
}

// Close must release the transports, or a second Open fails with a busy line.
func TestDeviceCloseReleasesTransports(t *testing.T) {
	o := stubTransports(t, realBoard(), nil)

	dev, err := OpenWith(Options{})
	if err != nil {
		t.Fatalf("OpenWith(): %v", err)
	}
	if err := dev.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}

	// Both transports share one fake here, so it is closed twice — once for
	// SPI and once for the GPIO lines. Releasing neither is what makes a
	// second Open fail with a busy line and misleading advice.
	if o.bus.closed != 2 {
		t.Errorf("the transports were closed %d times, want 2 (SPI and GPIO)", o.bus.closed)
	}

	// And Show after Close must report rather than touch the transport.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := dev.Show(ctx, dev.NewImage()); !errors.Is(err, epaper.ErrClosed) {
		t.Errorf("Show() after Close = %v, want ErrClosed", err)
	}
}

package inky

import (
	"context"
	"errors"
	"image"
	"strings"
	"testing"
	"time"

	"os"
	"path/filepath"
	"reflect"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/driver/jd79661"
	"github.com/sweeney/epaper/driver/jd79668"
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

// --------------------------------------------------------------------------
// Two panels, two controllers
// --------------------------------------------------------------------------

// realPHat is our Inky pHAT 2.13"'s EEPROM record, decoded. Captured from the
// board on 2026-09-16; the bytes are testdata/eeprom/phat-jd79661.bin.
func realPHat() *EEPROM {
	return &EEPROM{
		Width: 250, Height: 122,
		Colour: "red/yellow", PCBVariant: 100,
		DisplayVariant: variantRedYellowPHatJD79661,
		Model:          "Red/Yellow pHAT (JD79661)",
		WriteTime:      "2026-04-15 23:32:34.2",
	}
}

// The EEPROM picks the controller driver. Both boards are "red/yellow" with
// the same pin map and the same palette, so nothing downstream of here can
// tell them apart — if the dispatch is wrong, the symptom is a panel that
// stays blank, not an error.
func TestOpenWithDispatchesOnTheDisplayVariant(t *testing.T) {
	for _, tc := range []struct {
		name   string
		info   *EEPROM
		bounds image.Rectangle
		driver any
	}{
		{"wHAT 4.2", realBoard(), image.Rect(0, 0, 400, 300), (*jd79668.Device)(nil)},
		{"pHAT 2.13", realPHat(), image.Rect(0, 0, 250, 122), (*jd79661.Device)(nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubTransports(t, tc.info, nil)

			dev, err := OpenWith(Options{})
			if err != nil {
				t.Fatalf("OpenWith(): %v", err)
			}
			defer dev.Close()

			if got, want := reflect.TypeOf(dev), reflect.TypeOf(tc.driver); got != want {
				t.Errorf("OpenWith() returned %v, want %v", got, want)
			}
			if got := dev.Bounds(); got != tc.bounds {
				t.Errorf("Bounds() = %v, want %v", got, tc.bounds)
			}
			if got := dev.Model(); got != tc.info.Model {
				t.Errorf("Model() = %q, want the EEPROM's name", got)
			}
			for _, ink := range []epaper.Ink{epaper.Black, epaper.White, epaper.Yellow, epaper.Red} {
				if !dev.Palette().Has(ink) {
					t.Errorf("palette has no %s", ink)
				}
			}
		})
	}
}

// The pHAT is landscape: 250 wide by 122 tall. Its controller wants the frame
// the other way up, and that rotation is the driver's business — a caller that
// sees 122x250 here would draw everything sideways.
func TestOpenWithPresentsThePHatAsLandscape(t *testing.T) {
	stubTransports(t, realPHat(), nil)

	dev, err := OpenWith(Options{})
	if err != nil {
		t.Fatalf("OpenWith(): %v", err)
	}
	defer dev.Close()

	b := dev.Bounds()
	if b.Dx() <= b.Dy() {
		t.Errorf("Bounds() = %v, want landscape — the rotation belongs in the driver", b)
	}
	if img := dev.NewImage(); img.Bounds() != b {
		t.Errorf("NewImage() bounds = %v, want %v", img.Bounds(), b)
	}
}

// Options reach whichever driver was chosen, not just the first one.
//
// BusyTimeout is the one with a cheap observable: against a fake bus that
// never reports ready, a driver that got the option gives up when told to, and
// one that did not sits on the 40 s default.
func TestOpenWithPassesOptionsToThePHatDriver(t *testing.T) {
	o := stubTransports(t, realPHat(), nil)
	o.bus.busy = []int{busyBusy} // never reports ready

	dev, err := OpenWith(Options{BusyTimeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatalf("OpenWith(): %v", err)
	}
	defer dev.Close()

	started := time.Now()
	err = dev.Show(context.Background(), dev.NewImage())
	if err == nil {
		t.Fatal("Show() = nil against a fake bus that never reports ready")
	}
	if !errors.Is(err, ErrBusyTimeout) {
		t.Errorf("Show() = %v, want ErrBusyTimeout", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("Show() took %s to give up; the BusyTimeout option did not reach the driver", elapsed)
	}
}

// The whole path, from the bytes actually on the boards to the driver that
// ends up handling them: fixture -> ParseEEPROM -> driverFor -> Device.
//
// The pieces are each tested above, but only against each other. This is the
// one test where the display-variant numbers are pinned to something outside
// the source — captured EEPROM images — so that renumbering a constant cannot
// quietly agree with itself.
func TestTheRealBoardsReachTheRightDrivers(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		driver  any
		bounds  image.Rectangle
	}{
		{"what-jd79668.bin", (*jd79668.Device)(nil), image.Rect(0, 0, 400, 300)},
		{"phat-jd79661.bin", (*jd79661.Device)(nil), image.Rect(0, 0, 250, 122)},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "testdata", "eeprom", tc.fixture))
			if err != nil {
				t.Fatalf("reading fixture: %v", err)
			}
			info, err := ParseEEPROM(raw)
			if err != nil {
				t.Fatalf("ParseEEPROM(): %v", err)
			}
			stubTransports(t, info, nil)

			dev, err := OpenWith(Options{})
			if err != nil {
				t.Fatalf("OpenWith(): %v", err)
			}
			defer dev.Close()

			if got, want := reflect.TypeOf(dev), reflect.TypeOf(tc.driver); got != want {
				t.Errorf("display variant %d got %v, want %v", info.DisplayVariant, got, want)
			}
			if got := dev.Bounds(); got != tc.bounds {
				t.Errorf("Bounds() = %v, want %v", got, tc.bounds)
			}
		})
	}
}

// SupportedPanels and the dispatch must agree. They are two lists of the same
// thing, which is exactly the shape that drifts: a driver added to one and not
// the other gives a panel that opens but cannot be previewed, or one that is
// advertised and then refused.
func TestSupportedPanelsMatchesTheDispatch(t *testing.T) {
	listed := map[uint8]Panel{}
	for _, p := range SupportedPanels() {
		if _, dup := listed[p.DisplayVariant]; dup {
			t.Errorf("display variant %d is listed twice", p.DisplayVariant)
		}
		listed[p.DisplayVariant] = p
	}

	// Everything listed must have a driver...
	for v, p := range listed {
		if _, ok := driverFor(v); !ok {
			t.Errorf("SupportedPanels lists %q (variant %d) but OpenWith has no driver for it", p.Model, v)
		}
		if p.Model == "" || p.Controller == "" {
			t.Errorf("variant %d is listed with an empty model or controller: %+v", v, p)
		}
		if p.Width <= 0 || p.Height <= 0 {
			t.Errorf("%q is listed as %dx%d", p.Model, p.Width, p.Height)
		}
		// The name must be the vendor's, not one invented here.
		if int(v) >= len(displayVariants) || displayVariants[v] != p.Model {
			t.Errorf("variant %d is listed as %q but the vendor table says %q",
				v, p.Model, displayVariants[v])
		}
	}

	// ...and every driver must be listed. Sweeping the whole byte range is
	// cheap and needs no second copy of the variant numbers.
	for v := 0; v < 256; v++ {
		if _, ok := driverFor(uint8(v)); !ok {
			continue
		}
		if _, listed := listed[uint8(v)]; !listed {
			t.Errorf("OpenWith drives variant %d but SupportedPanels does not list it", v)
		}
	}
}

// The nominal geometry has to match what the real boards report, or a preview
// rendered from SupportedPanels is not a preview of anything.
func TestSupportedPanelGeometryMatchesTheRealEEPROMs(t *testing.T) {
	for _, f := range []string{"what-jd79668.bin", "phat-jd79661.bin"} {
		raw, err := os.ReadFile(filepath.Join("..", "testdata", "eeprom", f))
		if err != nil {
			t.Fatalf("reading fixture: %v", err)
		}
		info, err := ParseEEPROM(raw)
		if err != nil {
			t.Fatalf("ParseEEPROM(%s): %v", f, err)
		}
		var found bool
		for _, p := range SupportedPanels() {
			if p.DisplayVariant != info.DisplayVariant {
				continue
			}
			found = true
			if p.Width != info.Width || p.Height != info.Height {
				t.Errorf("%s: listed as %dx%d, the board reports %dx%d",
					p.Model, p.Width, p.Height, info.Width, info.Height)
			}
		}
		if !found {
			t.Errorf("%s: variant %d is not in SupportedPanels", f, info.DisplayVariant)
		}
	}
}

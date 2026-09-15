package inky

import (
	"errors"
	"fmt"
	"time"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/driver/jd79668"
	"github.com/sweeney/epaper/internal/gpiocdev"
	"github.com/sweeney/epaper/internal/i2c"
	"github.com/sweeney/epaper/internal/spidev"
)

// Errors this package returns in addition to the EEPROM ones. Match with
// [errors.Is].
var (
	// ErrChipSelectBusy means the kernel SPI driver owns the chip-select
	// line, so the panel cannot be addressed. The fix is a device-tree
	// overlay; the error message says which.
	ErrChipSelectBusy = errors.New("inky: chip-select line is claimed by the kernel SPI driver")

	// ErrBusyTimeout means the panel did not report itself ready in time.
	// Usually a refresh that genuinely stalled, or a BUSY line that is not
	// connected.
	ErrBusyTimeout = errors.New("inky: timed out waiting for the panel")

	// ErrUnsupportedPanel means a panel was identified but this library has
	// no driver for its controller. The error names the panel.
	ErrUnsupportedPanel = errors.New("inky: no driver for this panel")
)

// Pins is the GPIO line assignment for a board.
type Pins struct {
	// Reset is the active-low hardware reset line.
	Reset int
	// Busy is read to tell when the panel has finished. Low means busy.
	Busy int
	// DataCommand selects whether a byte is a command (low) or data (high).
	DataCommand int
	// ChipSelect is driven as a GPIO, active low, not by the SPI peripheral.
	ChipSelect int
}

// DefaultPins is Pimoroni's assignment for the Inky HAT range.
//
// From inky_jd79668.py: RESET_PIN = 27, BUSY_PIN = 17, DC_PIN = 22, CS_PIN = 8.
var DefaultPins = Pins{Reset: 27, Busy: 17, DataCommand: 22, ChipSelect: 8}

// Default device paths and bus settings.
const (
	DefaultSPIPath = "/dev/spidev0.0"
	DefaultI2CPath = "/dev/i2c-1"

	// DefaultSPISpeedHz is the vendor's clock rate.
	DefaultSPISpeedHz = 1_000_000

	// eepromAddr is the I2C address of the HAT identification EEPROM.
	eepromAddr = 0x50
)

// Options tunes [OpenWith]. The zero value is what [Open] uses.
type Options struct {
	// SPIPath, I2CPath and Pins override the board defaults.
	SPIPath string
	I2CPath string
	Pins    *Pins

	// SPISpeedHz overrides the clock rate. Zero means [DefaultSPISpeedHz].
	SPISpeedHz uint32

	// CommandDelay is the pause before each controller command. Zero means
	// none, which is this driver's default and makes a refresh about 4.9 s
	// faster than the reference implementation. Set it to
	// [jd79668.VendorCommandDelay] to restore the vendor's timing; see there
	// for the evidence, and PLAN §9.3 for the full case.
	CommandDelay time.Duration

	// BusyTimeout bounds a single wait for the panel. Zero means the
	// driver's default of 40 s.
	BusyTimeout time.Duration
}

// Open finds the attached Inky panel and returns a device for it.
//
// It reads the HAT's EEPROM to work out which panel is present, claims the
// GPIO lines, opens the SPI bus and hands back a driver for the right
// controller. Nothing is drawn and the panel is not touched.
//
// The caller must be in the spi, i2c and gpio groups. Root is not needed.
func Open() (epaper.Device, error) {
	return OpenWith(Options{})
}

// OpenWith is [Open] with the board defaults overridden.
func OpenWith(opts Options) (epaper.Device, error) {
	spiPath := orDefault(opts.SPIPath, DefaultSPIPath)
	i2cPath := orDefault(opts.I2CPath, DefaultI2CPath)
	pins := DefaultPins
	if opts.Pins != nil {
		pins = *opts.Pins
	}
	speed := opts.SPISpeedHz
	if speed == 0 {
		speed = DefaultSPISpeedHz
	}

	info, err := Identify(i2cPath)
	if err != nil {
		return nil, err
	}

	// Only one controller so far. Adding another means a new driver package
	// and one more case here — see CONTRIBUTING.md.
	if info.DisplayVariant != variantRedYellowWhatJD79668 {
		return nil, fmt.Errorf("inky: %q (display variant %d) is not one this library drives yet: %w",
			info.Model, info.DisplayVariant, ErrUnsupportedPanel)
	}

	lines, err := gpiocdev.Open(gpiocdev.Config{
		Consumer: "epaper",
		// Initial values matter: chip select and reset are active low and
		// must start released, or the panel sees a spurious pulse between
		// the line being claimed and the driver first driving it.
		Outputs: map[int]int{
			pins.ChipSelect:  release,
			pins.Reset:       release,
			pins.DataCommand: levelCommand,
		},
		Inputs: []int{pins.Busy},
	})
	if err != nil {
		return nil, gpioError(pins, err)
	}

	spi, err := spidev.Open(spidev.Config{Path: spiPath, Mode: spidev.Mode0, SpeedHz: speed})
	if err != nil {
		// Give the lines back; the SPI failure is the one worth reporting.
		_ = lines.Close()
		return nil, err
	}

	c := &conn{spi: spi, gpio: lines, pins: pins}
	dev, err := jd79668.New(c, jd79668.Config{
		Width:        info.Width,
		Height:       info.Height,
		Model:        info.Model,
		CommandDelay: opts.CommandDelay,
		BusyTimeout:  opts.BusyTimeout,
	})
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("inky: %w", err)
	}
	return dev, nil
}

// Identify reads and decodes the HAT's identification EEPROM.
//
// Useful on its own for working out what is plugged in without claiming any
// other hardware.
func Identify(i2cPath string) (*EEPROM, error) {
	bus, err := i2c.Open(i2cPath)
	if err != nil {
		return nil, fmt.Errorf("inky: %w", err)
	}
	// Read-only, so a close failure tells us nothing we can act on.
	defer func() { _ = bus.Close() }()

	raw, err := bus.ReadReg16(eepromAddr, 0x0000, eepromSize)
	if err != nil {
		return nil, fmt.Errorf("inky: reading the identification EEPROM at 0x%02X "+
			"(is a HAT attached, and is I2C enabled?): %w", eepromAddr, err)
	}
	return ParseEEPROM(raw)
}

// variantRedYellowWhatJD79668 is the only display variant with a driver.
const variantRedYellowWhatJD79668 = 24

// gpioError turns a line-request failure into advice.
//
// The chip-select case is worth special handling: without
// dtoverlay=spi0-0cs the kernel SPI driver owns GPIO 8, and the failure
// presents as a busy line rather than as a missing device. Working that out
// from first principles is a twenty-minute debugging session, so the error
// says it outright.
func gpioError(pins Pins, err error) error {
	if errors.Is(err, gpiocdev.ErrLineBusy) {
		return fmt.Errorf("inky: GPIO %d (chip select) is already claimed. "+
			"Add \"dtoverlay=spi0-0cs\" to /boot/firmware/config.txt and reboot: %w\n"+
			"underlying error: %v", pins.ChipSelect, ErrChipSelectBusy, err)
	}
	return fmt.Errorf("inky: claiming GPIO lines "+
		"(is the user in the gpio group?): %w", err)
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// ChipSelectAdvice returns the error [Open] gives when the chip-select line is
// already claimed. It is exported so the advice can be tested without a Pi
// that is deliberately misconfigured.
func ChipSelectAdvice(pins Pins) error {
	return gpioError(pins, gpiocdev.ErrLineBusy)
}

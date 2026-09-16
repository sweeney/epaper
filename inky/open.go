package inky

import (
	"errors"
	"fmt"
	"time"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/driver/jd79661"
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

// The transports, indirected so the wiring above them can be tested.
//
// What OpenWith does between reading the EEPROM and returning a device is
// exactly the part worth checking and the hardest to check on hardware: which
// pins get claimed, at what INITIAL levels — claiming reset or chip select
// low would pulse the panel before the driver has said anything — and whether
// the panel's own geometry reaches the driver. None of that produces an error
// when it is wrong; it produces a panel that misbehaves.
//
// Wrapped rather than assigned directly so that a nil *gpiocdev.Lines cannot
// become a non-nil interface holding a nil pointer.
var (
	openGPIO = func(cfg gpiocdev.Config) (gpioLines, error) {
		l, err := gpiocdev.Open(cfg)
		if err != nil {
			return nil, err
		}
		return l, nil
	}
	openSPI = func(cfg spidev.Config) (spiWriter, error) {
		d, err := spidev.Open(cfg)
		if err != nil {
			return nil, err
		}
		return d, nil
	}
	identify = Identify
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

	info, err := identify(i2cPath)
	if err != nil {
		return nil, err
	}

	// Which driver, decided before anything is claimed: refusing a panel
	// after taking the GPIO lines would leave them held until the process
	// exits. Adding a controller means a new driver package and one more
	// case in newDriver — see CONTRIBUTING.md.
	build, ok := driverFor(info.DisplayVariant)
	if !ok {
		return nil, fmt.Errorf("inky: %q (display variant %d) is not one this library drives yet: %w",
			info.Model, info.DisplayVariant, ErrUnsupportedPanel)
	}

	lines, err := openGPIO(gpiocdev.Config{
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

	spi, err := openSPI(spidev.Config{Path: spiPath, Mode: spidev.Mode0, SpeedHz: speed})
	if err != nil {
		// Give the lines back; the SPI failure is the one worth reporting.
		_ = lines.Close()
		return nil, err
	}

	c := &conn{spi: spi, gpio: lines, pins: pins}
	dev, err := build(c, info, opts)
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("inky: %w", err)
	}
	return dev, nil
}

// Panel is a board this library can drive.
//
// It is what [SupportedPanels] reports. The geometry is nominal — at runtime
// the EEPROM is authoritative and is what reaches the driver — but it is
// enough to render a preview, or a test card, for a panel nobody has plugged
// in. That is what the test card's goldens use it for, so adding a driver adds
// its renders to CI without anyone remembering to.
type Panel struct {
	// DisplayVariant is the EEPROM byte that identifies this board.
	DisplayVariant uint8

	// Model is the vendor's name for it, as the EEPROM reports it.
	Model string

	// Controller names the chip behind the glass, which is what actually
	// determines the driver. Two boards with the same controller share one.
	Controller string

	// Width and Height are the panel's nominal geometry in pixels, in the
	// orientation the picture is drawn in.
	Width, Height int
}

// SupportedPanels returns every board this library has a driver for.
//
// Ordered by display variant, so the output is stable. The list and the
// dispatch in [OpenWith] are checked against each other by a test: a driver
// added to one and not the other is a panel that either cannot be opened or
// cannot be previewed.
func SupportedPanels() []Panel {
	return []Panel{
		{
			DisplayVariant: variantRedYellowPHatJD79661,
			Model:          displayVariants[variantRedYellowPHatJD79661],
			Controller:     "JD79661",
			Width:          250, Height: 122,
		},
		{
			DisplayVariant: variantRedYellowWhatJD79668,
			Model:          displayVariants[variantRedYellowWhatJD79668],
			Controller:     "JD79668",
			Width:          400, Height: 300,
		},
	}
}

// driverFor maps an EEPROM display variant to the driver that handles it.
//
// The two Pimoroni four-ink boards look identical from up here — both are
// "red/yellow", both use the same pins, both have the same palette in the same
// order — and they carry different controllers with different init sequences
// and different frame layouts. Nothing downstream can tell them apart, so if
// this picks wrong the symptom is a panel that stays blank rather than an
// error. That is why the dispatch is on the variant byte and nothing else.
func driverFor(variant uint8) (func(*conn, *EEPROM, Options) (epaper.Device, error), bool) {
	switch variant {
	case variantRedYellowPHatJD79661:
		return func(c *conn, info *EEPROM, opts Options) (epaper.Device, error) {
			return jd79661.New(c, jd79661.Config{
				Width:        info.Width,
				Height:       info.Height,
				Model:        info.Model,
				CommandDelay: opts.CommandDelay,
				BusyTimeout:  opts.BusyTimeout,
			})
		}, true
	case variantRedYellowWhatJD79668:
		return func(c *conn, info *EEPROM, opts Options) (epaper.Device, error) {
			return jd79668.New(c, jd79668.Config{
				Width:        info.Width,
				Height:       info.Height,
				Model:        info.Model,
				CommandDelay: opts.CommandDelay,
				BusyTimeout:  opts.BusyTimeout,
			})
		}, true
	}
	return nil, false
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

// The display variants this library has drivers for, from the vendor's table
// in eeprom.go. Both boards are four-ink red/yellow panels; they differ in the
// controller behind the glass, which is the whole reason there are two
// drivers.
const (
	variantRedYellowPHatJD79661 = 23 // Inky pHAT 2.13", 250x122
	variantRedYellowWhatJD79668 = 24 // Inky wHAT 4.2",  400x300
)

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

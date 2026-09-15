// Package jd79668 drives e-ink panels built around the JD79668 controller.
//
// The controller is made by Fitipower and rebadged by board vendors, so this
// driver is not specific to any one product: it will drive any board carrying
// a JD79668, and only the pin map and the detection mechanism differ. The
// board-specific parts live elsewhere — see the inky package for Pimoroni's.
//
// # Where the magic numbers come from
//
// Every command code, payload byte and delay in this file is derived from
// Pimoroni's inky library version 2.5.0, specifically inky_jd79668.py, which
// is reproduced at reference/vendor-inky/ so a reader can check any constant
// against its origin. That work is MIT licensed; see NOTICE.
//
// Nothing here was guessed. Where a constant is unexplained in the vendor
// source it is unexplained here too, and said to be.
//
// # Four inks, not three
//
// This panel shows black, white, yellow and red simultaneously. The board's
// own documentation described it as three-colour with a single accent, which
// is wrong: "red/yellow" in the EEPROM means both at once.
package jd79668

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"sync"
	"time"

	"github.com/sweeney/epaper"
)

// Palette is what this controller can display, in the order the controller
// indexes them.
//
// THE ORDER IS WIRE-SIGNIFICANT. Slice position is the two-bit value sent in
// the framebuffer: black is 0, white is 1, yellow is 2, red is 3. Reordering
// this for tidiness would silently swap the colours on every panel using this
// driver, and every test would still pass except the one that pins it.
//
// The RGB values are for previews and PNG output only; they are never sent to
// the controller. They are chosen to resemble what the panel actually puts on
// the glass, which is a muted yellow and a brick red — not the pure #FFFF00
// and #FF0000 the vendor library uses as quantisation targets.
//
// Index order from inky_jd79668.py: BLACK = 0, WHITE = 1, YELLOW = 2, RED = 3.
var Palette = epaper.Palette{
	{Ink: epaper.Black, RGB: color.RGBA{0, 0, 0, 255}},
	{Ink: epaper.White, RGB: color.RGBA{255, 255, 255, 255}},
	{Ink: epaper.Yellow, RGB: color.RGBA{235, 205, 40, 255}},
	{Ink: epaper.Red, RGB: color.RGBA{190, 45, 40, 255}},
}

// Controller command codes.
//
// From inky_jd79668.py. The glossary in PLAN §12 decodes them; the ones with
// no name here have none in the vendor source either.
const (
	cmdPSR   = 0x00 // Panel Setting Register
	cmdPOF   = 0x02 // Power OFF
	cmdPON   = 0x04 // Power ON
	cmdBTSTP = 0x06 // Booster Soft Start
	cmdDSLP  = 0x07 // Deep Sleep
	cmdDTM   = 0x10 // Data Transmission — the framebuffer
	cmdDRF   = 0x12 // Display Refresh — this is the 20-odd seconds
	cmdCDI   = 0x50 // VCOM and Data Interval
	cmdTRES  = 0x61 // Resolution Setting
)

// dslpMagic is the safety argument DSLP requires; a deep-sleep command without
// it is ignored.
const dslpMagic = 0xA5

// Conn is the transport a driver talks through.
//
// The driver depends on this interface rather than on a concrete SPI type,
// which is what lets the entire command sequence be tested with no hardware —
// see the recording test, which pins every byte this driver emits.
type Conn interface {
	// Command sends a command byte followed by its data payload, with the
	// data/command line set appropriately for each, in a single chip-select
	// assertion. data may be nil.
	//
	// There is deliberately no separate Data method. PLAN §6.2 sketched one,
	// but every byte this controller receives is part of a command — even
	// the 30,000-byte framebuffer, which is DTM's payload. An unused
	// interface method is dead code every implementation has to write.
	Command(cmd byte, data []byte) error

	// Reset pulses the hardware reset line.
	Reset(ctx context.Context) error

	// WaitReady blocks until the panel reports itself ready, or the timeout
	// or context expires.
	WaitReady(ctx context.Context, timeout time.Duration) error
}

// Defaults for [Config]. Each is the vendor's value unless noted.
const (
	// DefaultBusyTimeout bounds a single wait. A full refresh is about 20 s
	// measured; 40 s is the vendor's timeout and leaves comfortable margin.
	DefaultBusyTimeout = 40 * time.Second

	// DefaultMinRefreshTime is the shortest believable full refresh.
	//
	// This guards the one failure BUSY cannot report. The line has a host
	// pull-up, so a DISCONNECTED BUSY reads high — "ready" — and every wait
	// returns instantly. Show would then succeed in milliseconds having
	// drawn nothing, on a panel nobody is watching.
	//
	// Checking the line's state directly cannot distinguish that from a
	// genuinely quick reply, but the elapsed time can: a measured full
	// refresh on this panel is 20.5 s, and no real one completes in under a
	// second. So the driver times the refresh wait instead of interrogating
	// the line, which is unambiguous and has no race.
	DefaultMinRefreshTime = time.Second

	// DefaultCommandDelay is the pause before each command byte: none.
	//
	// The vendor driver sleeps 300 ms before EVERY command, which is about
	// 4.9 s of a 25.4 s refresh. We do not, and this is the one place this
	// driver knowingly departs from the reference. PLAN §9.3 has the full
	// case; in short:
	//
	//   - Measured: 25.6 s with the delay, 20.4 s without, repeatable to
	//     10 ms. A 10-refresh soak without it varied by 318 ms in total.
	//   - Inspected: the same 0.3 appears in three vendor drivers for
	//     unrelated controllers from different manufacturers, which is not
	//     how a datasheet-derived timing looks. In exactly those drivers,
	//     and no others, the shared _spi_write helper is defined but never
	//     called — the signature of someone inlining the SPI call to add a
	//     sleep while debugging, and the original never being removed.
	//   - Looked at: the output is indistinguishable on the panel.
	//
	// Set [Config.CommandDelay] to [VendorCommandDelay] to put it back. Do
	// that if a panel misbehaves, and please write down what it did.
	DefaultCommandDelay = 0

	// VendorCommandDelay is the pause the reference implementation uses. It
	// is here so restoring the vendor's exact timing is one word, not a
	// magic number rediscovered under pressure.
	VendorCommandDelay = 300 * time.Millisecond
)

// Config describes a panel built around this controller.
type Config struct {
	// Width and Height are the panel geometry in pixels.
	Width, Height int

	// Model is the name reported by [Device.Model]. Boards should pass the
	// name from their EEPROM; it defaults to the controller name.
	Model string

	// CommandDelay is the pause before each controller command. Zero or
	// less means none, which is [DefaultCommandDelay] and what this driver
	// does by default; see there for why. Use [VendorCommandDelay] to
	// restore the reference implementation's timing.
	CommandDelay time.Duration

	// BusyTimeout bounds a single wait for the panel. Zero means
	// [DefaultBusyTimeout].
	BusyTimeout time.Duration

	// MinRefreshTime is the shortest refresh treated as believable. Zero
	// means [DefaultMinRefreshTime]; a negative value disables the check,
	// which is what a test with a fake transport wants.
	MinRefreshTime time.Duration
}

// Device is a panel driven by a JD79668.
type Device struct {
	conn Conn
	cfg  Config

	bounds  image.Rectangle
	palette epaper.Palette

	// mu serialises Show. A service with a ticker and a webhook both
	// drawing would otherwise interleave two framebuffers into one picture,
	// 20 seconds later, intermittently — PLAN §9.6.
	mu     sync.Mutex
	closed bool
}

// New returns a driver for a panel of the given geometry.
//
// It does not touch the hardware: the panel is initialised at the start of
// every [Device.Show], because the previous one left it in deep sleep.
func New(conn Conn, cfg Config) (*Device, error) {
	if conn == nil {
		return nil, fmt.Errorf("jd79668: conn is nil")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, fmt.Errorf("jd79668: panel geometry %dx%d is not usable", cfg.Width, cfg.Height)
	}
	if cfg.Model == "" {
		cfg.Model = "JD79668"
	}
	if cfg.BusyTimeout == 0 {
		cfg.BusyTimeout = DefaultBusyTimeout
	}
	if cfg.MinRefreshTime == 0 {
		cfg.MinRefreshTime = DefaultMinRefreshTime
	}
	return &Device{
		conn:    conn,
		cfg:     cfg,
		bounds:  image.Rect(0, 0, cfg.Width, cfg.Height),
		palette: Palette,
	}, nil
}

// Model returns the panel's name.
func (d *Device) Model() string { return d.cfg.Model }

// Bounds returns the panel geometry.
func (d *Device) Bounds() image.Rectangle { return d.bounds }

// Palette returns the panel's inks, in controller index order.
func (d *Device) Palette() epaper.Palette { return d.palette }

// NewImage returns a blank image of the right size with the right palette.
func (d *Device) NewImage() *image.Paletted {
	return image.NewPaletted(d.bounds, d.palette.Colors())
}

// Show draws an image and returns once the refresh is complete, which takes
// around 20 seconds.
//
// Cancelling ctx abandons the wait, not the refresh. The controller has no
// abort: it finishes redrawing regardless, and the next Show will block until
// it has. The image is validated before anything reaches the hardware.
//
// Safe for concurrent use; concurrent callers queue.
func (d *Device) Show(ctx context.Context, img *image.Paletted) error {
	if img == nil {
		return fmt.Errorf("jd79668: show: image is nil: %w", epaper.ErrBadImage)
	}
	if img.Bounds() != d.bounds {
		return fmt.Errorf("jd79668: show: image is %v but the panel is %v: %w",
			img.Bounds(), d.bounds, epaper.ErrWrongSize)
	}
	if !d.samePalette(img) {
		return fmt.Errorf("jd79668: show: image palette is not the panel's "+
			"(use Device.NewImage or render.NewCanvasFor): %w", epaper.ErrPaletteMismatch)
	}

	// Pack before taking the lock and before touching the panel: a bad
	// image should fail without leaving the controller half-initialised.
	frame, err := epaper.Pack(img)
	if err != nil {
		return fmt.Errorf("jd79668: show: %w", err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return fmt.Errorf("jd79668: show: %w", epaper.ErrClosed)
	}
	return d.refresh(ctx, frame)
}

// refresh runs the full init-and-draw sequence.
func (d *Device) refresh(ctx context.Context, frame []byte) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("jd79668: show: %w", err)
	}
	if err := d.init(ctx); err != nil {
		return err
	}

	// The framebuffer, then power on, refresh, power off, sleep. Each of the
	// three power steps has to complete before the next is sent.
	if err := d.command(cmdDTM, frame); err != nil {
		return fmt.Errorf("jd79668: sending framebuffer: %w", err)
	}
	for _, step := range []struct {
		name string
		cmd  byte
		data []byte
	}{
		{"power on", cmdPON, nil},
		{"refresh", cmdDRF, []byte{0x00}},
		{"power off", cmdPOF, []byte{0x00}},
	} {
		if err := d.command(step.cmd, step.data); err != nil {
			return fmt.Errorf("jd79668: %s: %w", step.name, err)
		}
		started := time.Now()
		if err := d.conn.WaitReady(ctx, d.cfg.BusyTimeout); err != nil {
			return fmt.Errorf("jd79668: waiting after %s: %w", step.name, err)
		}
		if step.cmd == cmdDRF {
			if err := d.checkRefreshWasReal(time.Since(started)); err != nil {
				return err
			}
		}
	}

	// Deep sleep. Nothing waits on this; the panel is done, and the next
	// Show re-initialises from reset.
	if err := d.command(cmdDSLP, []byte{dslpMagic}); err != nil {
		return fmt.Errorf("jd79668: deep sleep: %w", err)
	}
	return nil
}

// checkRefreshWasReal reports an error if the panel claimed to finish
// redrawing implausibly fast. See [DefaultMinRefreshTime] for why this is
// timed rather than read off the BUSY line.
func (d *Device) checkRefreshWasReal(elapsed time.Duration) error {
	if d.cfg.MinRefreshTime < 0 || elapsed >= d.cfg.MinRefreshTime {
		return nil
	}
	return fmt.Errorf("jd79668: the panel reported the refresh complete after only %s, "+
		"but a full refresh takes about 20s. The BUSY line reads ready when it is "+
		"disconnected, so check its wiring — nothing was drawn: %w", elapsed, ErrBusyNotConnected)
}

// ErrBusyNotConnected means the panel reported a refresh finished far too
// quickly to be true, which on this hardware means the BUSY line is not
// actually connected. Match with [errors.Is].
var ErrBusyNotConnected = errors.New("jd79668: BUSY line appears disconnected")

// init resets the panel and sends the initialisation sequence.
//
// This runs before EVERY refresh, not once at startup, because the previous
// refresh ended with DSLP and the controller has forgotten everything.
//
// The reset comes first and unconditionally. BUSY reads busy for as long as
// reset is asserted, so a wait beforehand tells you nothing; and an idle panel
// is indistinguishable from a disconnected BUSY line, so there is nothing to
// check for either. Characterised on the bench — see PLAN §2.3.
//
// Payloads verbatim from inky_jd79668.py Inky.setup(). The commands without
// symbolic names have none in the vendor source; they are register writes the
// vendor does not explain, and inventing names for them here would imply an
// understanding nobody has.
func (d *Device) init(ctx context.Context) error {
	if err := d.conn.Reset(ctx); err != nil {
		return fmt.Errorf("jd79668: reset: %w", err)
	}

	w, h := d.cfg.Width, d.cfg.Height
	for _, c := range []struct {
		cmd  byte
		data []byte
	}{
		{0x4D, []byte{0x78}},
		{cmdPSR, []byte{0x0F, 0x29}},
		{cmdBTSTP, []byte{0x0D, 0x12, 0x24, 0x25, 0x12, 0x29, 0x10}},
		{0x30, []byte{0x08}},
		{cmdCDI, []byte{0x37}},
		// Resolution, as two big-endian 16-bit values. The vendor hardcodes
		// 0x01,0x90,0x01,0x2C, which is exactly 400 then 300.
		{cmdTRES, []byte{byte(w >> 8), byte(w), byte(h >> 8), byte(h)}},
		{0xAE, []byte{0xCF}},
		{0xB0, []byte{0x13}},
		{0xBD, []byte{0x07}},
		{0xBE, []byte{0xFE}},
		{0xE9, []byte{0x01}},
	} {
		if err := d.command(c.cmd, c.data); err != nil {
			return fmt.Errorf("jd79668: init command 0x%02X: %w", c.cmd, err)
		}
	}
	return nil
}

// command pauses, then sends. See DefaultCommandDelay for why the pause.
func (d *Device) command(cmd byte, data []byte) error {
	if d.cfg.CommandDelay > 0 {
		time.Sleep(d.cfg.CommandDelay)
	}
	return d.conn.Command(cmd, data)
}

// Close releases the transport. It is safe to call more than once.
func (d *Device) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	d.closed = true
	if c, ok := d.conn.(interface{ Close() error }); ok {
		return c.Close()
	}
	return nil
}

// samePalette reports whether an image's colours are this panel's, in order.
func (d *Device) samePalette(img *image.Paletted) bool {
	if len(img.Palette) != len(d.palette) {
		return false
	}
	for i, e := range d.palette {
		if img.Palette[i] != color.Color(e.RGB) {
			return false
		}
	}
	return true
}

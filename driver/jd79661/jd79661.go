// Package jd79661 drives e-ink panels built around the JD79661 controller.
//
// The controller is made by Fitipower and rebadged by board vendors, so this
// driver is not specific to any one product: it will drive any board carrying
// a JD79661, and only the pin map and the detection mechanism differ. The
// board-specific parts live elsewhere — see the inky package for Pimoroni's.
//
// In practice that means the Inky pHAT 2.13", which is 250x122 and shows
// black, white, yellow and red simultaneously.
//
// # Where the magic numbers come from
//
// Every command code, payload byte and delay in this file is derived from
// Pimoroni's inky library version 2.5.0, specifically inky_jd79661.py, which
// is reproduced at reference/vendor-inky/ so a reader can check any constant
// against its origin. That work is MIT licensed; see NOTICE.
//
// Nothing here was guessed. Where a constant is unexplained in the vendor
// source it is unexplained here too, and said to be.
//
// # This is not the JD79668 with different numbers
//
// The two controllers share a command vocabulary, a refresh sequence and a
// palette, which makes them look like one driver with a geometry parameter.
// They are not, and the temptation is worth naming because giving in to it
// produces a panel that draws nothing:
//
//   - The init sequences differ. This one writes PWR, POFS, TCON, PWS, 0xE7,
//     0xB6 and 0xB4, which the JD79668 does not; the JD79668 writes 0xAE,
//     0xB0, 0xBD and 0xBE, which this one does not. BTST_P exists in both with
//     a different payload.
//   - The frame layout differs, and that is the real split. The JD79668 sends
//     its buffer row-major. This one pads the short axis out to a multiple of
//     eight and sends the result rotated a quarter turn — see [Device.Show].
//
// TestInitIsNotTheJD79668Sequence exists to stop a future tidy-up from merging
// the two.
package jd79661

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
// the controller. They are the same renditions the JD79668 driver uses —
// measured against a wHAT, and close enough on this panel that the difference
// is not worth two sets of numbers. They are deliberately not the pure
// #FFFF00 and #FF0000 the vendor library uses as quantisation targets, which
// no four-ink panel actually produces.
//
// Index order from inky_jd79661.py: BLACK = 0, WHITE = 1, YELLOW = 2, RED = 3.
var Palette = epaper.Palette{
	{Ink: epaper.Black, RGB: color.RGBA{0, 0, 0, 255}},
	{Ink: epaper.White, RGB: color.RGBA{255, 255, 255, 255}},
	{Ink: epaper.Yellow, RGB: color.RGBA{235, 205, 40, 255}},
	{Ink: epaper.Red, RGB: color.RGBA{190, 45, 40, 255}},
}

// Controller command codes.
//
// From inky_jd79661.py. The ones with no name here have none in the vendor
// source either.
const (
	cmdPSR   = 0x00 // Panel Setting Register
	cmdPWR   = 0x01 // Power Setting
	cmdPOF   = 0x02 // Power OFF
	cmdPOFS  = 0x03 // Power OFF Sequence
	cmdPON   = 0x04 // Power ON
	cmdBTSTP = 0x06 // Booster Soft Start
	cmdDSLP  = 0x07 // Deep Sleep
	cmdDTM   = 0x10 // Data Transmission — the framebuffer
	cmdDRF   = 0x12 // Display Refresh — this is the ten-odd seconds
	cmdCDI   = 0x50 // VCOM and Data Interval
	cmdTCON  = 0x60 // TCON Setting
	cmdTRES  = 0x61 // Resolution Setting
	cmdPWS   = 0xE3 // Power Saving
)

// dslpMagic is the safety argument DSLP requires; a deep-sleep command without
// it is ignored.
const dslpMagic = 0xA5

// The wire format: two bits per pixel, four pixels to a byte, most significant
// pixel first. Same as the JD79668 — only the order the pixels are visited
// differs, which is why this driver packs its own frame instead of calling
// [epaper.Pack].
const (
	bitsPerPixel  = 2
	pixelsPerByte = 8 / bitsPerPixel

	// fastAxisMultiple is what the controller's fast axis is rounded up to.
	// The vendor pads 122 to 128 with six rows of overscan and sets TRES to
	// match, so the rounding is to eight; 122 is the only case the reference
	// exercises, and TestResolutionCommandFollowsTheGeometry pins it.
	fastAxisMultiple = 8
)

// Conn is the transport a driver talks through.
//
// The driver depends on this interface rather than on a concrete SPI type,
// which is what lets the entire command sequence be tested with no hardware —
// see the recording test, which pins every byte this driver emits.
type Conn interface {
	// Command sends a command byte followed by its data payload, with the
	// data/command line set appropriately for each, in a single chip-select
	// assertion. data may be nil.
	Command(cmd byte, data []byte) error

	// Reset pulses the hardware reset line.
	Reset(ctx context.Context) error

	// WaitReady blocks until the panel reports itself ready, or the timeout
	// or context expires.
	WaitReady(ctx context.Context, timeout time.Duration) error
}

// Defaults for [Config]. Each is the vendor's value unless noted.
const (
	// DefaultBusyTimeout bounds a single wait. 40 s is the vendor's timeout
	// and leaves comfortable margin over a measured refresh.
	DefaultBusyTimeout = 40 * time.Second

	// DefaultMinRefreshTime is the shortest believable full refresh.
	//
	// This guards the one failure BUSY cannot report. The line has a host
	// pull-up, so a DISCONNECTED BUSY reads high — "ready" — and every wait
	// returns instantly. Show would then succeed in milliseconds having
	// drawn nothing, on a panel nobody is watching.
	//
	// Checking the line's state directly cannot distinguish that from a
	// genuinely quick reply, but the elapsed time can. A second is well under
	// any real refresh on this panel and well over any wiring fault.
	DefaultMinRefreshTime = time.Second

	// DefaultCommandDelay is the pause before each command byte: none.
	//
	// The vendor driver sleeps 300 ms before EVERY command. This driver does
	// not, for the reasons measured on the JD79668 and set out in PLAN §9.3 —
	// the sleep is a debugging leftover, not a datasheet timing, and the same
	// 0.3 appears in the vendor's drivers for unrelated controllers from
	// different manufacturers.
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
	// Width and Height are the panel geometry in pixels, as the picture is
	// presented: 250x122 for the Inky pHAT 2.13". The controller's own,
	// rotated geometry is derived from these and is not a caller's concern.
	Width, Height int

	// Model is the name reported by [Device.Model]. Boards should pass the
	// name from their EEPROM; it defaults to the controller name.
	Model string

	// CommandDelay is the pause before each controller command. Zero or
	// less means none, which is [DefaultCommandDelay] and what this driver
	// does by default; see there for why.
	CommandDelay time.Duration

	// BusyTimeout bounds a single wait for the panel. Zero means
	// [DefaultBusyTimeout].
	BusyTimeout time.Duration

	// MinRefreshTime is the shortest refresh treated as believable. Zero
	// means [DefaultMinRefreshTime]; a negative value disables the check,
	// which is what a test with a fake transport wants.
	MinRefreshTime time.Duration
}

// Device is a panel driven by a JD79661.
type Device struct {
	conn Conn
	cfg  Config

	bounds  image.Rectangle
	palette epaper.Palette

	// The controller's frame, as opposed to the panel's picture. ctrlW is
	// the fast axis — the panel's height, padded — and ctrlH the slow one.
	// See [Device.Show] for the mapping between the two.
	ctrlW, ctrlH int

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
		return nil, fmt.Errorf("jd79661: conn is nil")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, fmt.Errorf("jd79661: panel geometry %dx%d is not usable", cfg.Width, cfg.Height)
	}
	if cfg.Model == "" {
		cfg.Model = "JD79661"
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
		ctrlW:   roundUp(cfg.Height, fastAxisMultiple),
		ctrlH:   cfg.Width,
	}, nil
}

// Model returns the panel's name.
func (d *Device) Model() string { return d.cfg.Model }

// Bounds returns the panel geometry, in the orientation the picture is drawn
// in: 250x122 landscape, not the controller's 128x250.
func (d *Device) Bounds() image.Rectangle { return d.bounds }

// Palette returns the panel's inks, in controller index order.
func (d *Device) Palette() epaper.Palette { return d.palette }

// NewImage returns a blank image of the right size with the right palette.
func (d *Device) NewImage() *image.Paletted {
	return image.NewPaletted(d.bounds, d.palette.Colors())
}

// Show draws an image and returns once the refresh is complete.
//
// Cancelling ctx abandons the wait, not the refresh. The controller has no
// abort: it finishes redrawing regardless, and the next Show will block until
// it has. The image is validated before anything reaches the hardware.
//
// Safe for concurrent use; concurrent callers queue.
//
// # The frame this sends
//
// The picture is landscape and the controller's frame is portrait, so Show
// rotates. The rule, derived from Inky.show() in inky_jd79661.py and verified
// against numpy index by index (PLAN §13), is:
//
//	controller (cx, cy)  <-  image (x = cy, y = H-1-cx)
//
// with cx >= H — the padding that rounds the panel's 122 rows up to the
// controller's 128 — sent as black. The vendor expresses the same thing as a
// vstack of six blank rows followed by numpy.rot90(region, -1); this is that
// expression with the intermediate arrays taken out.
//
// For the 2.13" panel that is 32,000 pixels in 8,000 bytes, of which 30,500
// are visible. The 1,500 padding pixels address no glass.
func (d *Device) Show(ctx context.Context, img *image.Paletted) error {
	if img == nil {
		return fmt.Errorf("jd79661: show: image is nil: %w", epaper.ErrBadImage)
	}
	if img.Bounds() != d.bounds {
		return fmt.Errorf("jd79661: show: image is %v but the panel is %v: %w",
			img.Bounds(), d.bounds, epaper.ErrWrongSize)
	}
	if !d.samePalette(img) {
		return fmt.Errorf("jd79661: show: image palette is not the panel's "+
			"(use Device.NewImage or render.NewCanvasFor): %w", epaper.ErrPaletteMismatch)
	}

	// Pack before taking the lock and before touching the panel: a bad
	// image should fail without leaving the controller half-initialised.
	frame, err := d.frame(img)
	if err != nil {
		return fmt.Errorf("jd79661: show: %w", err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return fmt.Errorf("jd79661: show: %w", epaper.ErrClosed)
	}
	return d.refresh(ctx, frame)
}

// frame converts the picture into the controller's framebuffer. See
// [Device.Show] for the mapping and where it comes from.
func (d *Device) frame(img *image.Paletted) ([]byte, error) {
	h := d.cfg.Height
	limit := uint8(len(img.Palette))
	out := make([]byte, d.ctrlW*d.ctrlH/pixelsPerByte)

	// The byte under construction is accumulated in a register and stored
	// once rather than OR-ed into out four times; epaper.Pack measured that
	// at about 1.8x faster, and the reasoning carries over.
	var cur byte
	n := 0

	for cy := range d.ctrlH {
		// cy is the image's x. Walking it as the outer loop is the
		// controller's order, not the image's, so this reads down a column
		// and strides rather than running along a row. At 32,000 pixels
		// against a refresh measured in seconds, that is not worth
		// reorganising for.
		for cx := range d.ctrlW {
			var v uint8
			if cx < h {
				// PixOffset rather than y*Stride+x: the two agree only
				// because Show has already checked the bounds are the
				// panel's, and so anchored at (0,0). Going through it anyway
				// costs nothing and removes the assumption.
				v = img.Pix[img.PixOffset(cy, h-1-cx)]
				if v >= limit {
					return nil, fmt.Errorf("pixel (%d,%d) has index %d but the palette defines %d colours: %w",
						cy, h-1-cx, v, limit, epaper.ErrBadImage)
				}
			}
			// 0, 1, 2, 3 pixels into the byte shift by 6, 4, 2, 0.
			cur |= v << ((pixelsPerByte - 1 - n%pixelsPerByte) * bitsPerPixel)
			n++
			if n%pixelsPerByte == 0 {
				out[n/pixelsPerByte-1] = cur
				cur = 0
			}
		}
	}
	return out, nil
}

// refresh runs the full init-and-draw sequence.
func (d *Device) refresh(ctx context.Context, frame []byte) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("jd79661: show: %w", err)
	}
	if err := d.init(ctx); err != nil {
		return err
	}

	// The framebuffer, then power on, refresh, power off, sleep. Each of the
	// three power steps has to complete before the next is sent.
	if err := d.command(cmdDTM, frame); err != nil {
		return fmt.Errorf("jd79661: sending framebuffer: %w", err)
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
			return fmt.Errorf("jd79661: %s: %w", step.name, err)
		}
		started := time.Now()
		if err := d.conn.WaitReady(ctx, d.cfg.BusyTimeout); err != nil {
			return fmt.Errorf("jd79661: waiting after %s: %w", step.name, err)
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
		return fmt.Errorf("jd79661: deep sleep: %w", err)
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
	return fmt.Errorf("jd79661: the panel reported the refresh complete after only %s, "+
		"but a full refresh takes several seconds. The BUSY line reads ready when it is "+
		"disconnected, so check its wiring — nothing was drawn: %w", elapsed, ErrBusyNotConnected)
}

// ErrBusyNotConnected means the panel reported a refresh finished far too
// quickly to be true, which on this hardware means the BUSY line is not
// actually connected. Match with [errors.Is].
var ErrBusyNotConnected = errors.New("jd79661: BUSY line appears disconnected")

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
// Payloads verbatim from inky_jd79661.py Inky.setup(), in its order. The
// commands without symbolic names have none in the vendor source; they are
// register writes the vendor does not explain, and inventing names for them
// here would imply an understanding nobody has.
func (d *Device) init(ctx context.Context) error {
	if err := d.conn.Reset(ctx); err != nil {
		return fmt.Errorf("jd79661: reset: %w", err)
	}

	for _, c := range []struct {
		cmd  byte
		data []byte
	}{
		{0x4D, []byte{0x78}},
		{cmdPSR, []byte{0x0F, 0x29}},
		{cmdPWR, []byte{0x07, 0x00}},
		{cmdPOFS, []byte{0x10, 0x54, 0x44}},
		{cmdBTSTP, []byte{0x0F, 0x0A, 0x2F, 0x25, 0x22, 0x2E, 0x21}},
		{cmdCDI, []byte{0x37}},
		{cmdTCON, []byte{0x02, 0x02}},
		// Resolution, as two big-endian 16-bit values, and NOT the panel's
		// geometry: the controller is told about its own rotated, padded
		// frame. The vendor hardcodes 0x00,0x80,0x00,0xFA, which is exactly
		// 128 then 250 for a 250x122 panel.
		{cmdTRES, []byte{byte(d.ctrlW >> 8), byte(d.ctrlW), byte(d.ctrlH >> 8), byte(d.ctrlH)}},
		{0xE7, []byte{0x1C}},
		{cmdPWS, []byte{0x22}},
		{0xB6, []byte{0x6F}},
		{0xB4, []byte{0xD0}},
		{0xE9, []byte{0x01}},
		{0x30, []byte{0x08}},
	} {
		if err := d.command(c.cmd, c.data); err != nil {
			return fmt.Errorf("jd79661: init command 0x%02X: %w", c.cmd, err)
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

// roundUp rounds n up to the next multiple of m.
func roundUp(n, m int) int { return (n + m - 1) / m * m }

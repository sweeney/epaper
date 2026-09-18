// Package e640 drives Spectra 6 e-ink panels built around the E640 controller.
//
// In practice that means the Pimoroni Inky Impression 4.0", which is 600x400
// and shows six inks simultaneously: black, white, yellow, red, blue and
// green. As with the other drivers in this module, the board-specific parts —
// pins, detection — live elsewhere; see the inky package.
//
// # Where the magic numbers come from
//
// Every command code, payload byte and wire value in this file is derived from
// Pimoroni's inky library version 2.5.0, specifically inky_e640.py, which is
// reproduced at reference/vendor-inky/ so a reader can check any constant
// against its origin. That work is MIT licensed; see NOTICE.
//
// The vendor source names the registers (PSR, PWR, BTST1 and so on) but
// explains none of the payloads, and neither does this file. Where a value is
// unexplained there it is unexplained here too, and said to be.
//
// # How it differs from the JD controllers
//
// Enough that nothing is shared but the shape of the refresh:
//
//   - Four bits per pixel, two pixels to a byte, high nibble first — not two
//     bits and four.
//   - The wire values skip 4. Blue is sent as 5 and green as 6, so the
//     palette's slice position is NOT the wire value, which makes this the one
//     driver where that library-wide rule has an exception. See [Palette].
//   - The frame is sent rotated a quarter turn, like the JD79661's, but with
//     no padding.
//   - The refresh reprograms the booster between power-on and refresh, and
//     ends with power-off rather than deep sleep.
package e640

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

// Palette is what this controller can display, in the vendor's order.
//
// Unlike every other driver in this module, slice position is NOT the value
// sent to the controller: the controller's colour codes skip 4, and
// [Device.Show] translates through wireValue. The order here is still fixed —
// it is the order inky_e640.py numbers its inks (Inky.BLACK = 0 through
// Inky.GREEN = 5) and the order its set_image() quantises into — and
// TestPaletteOrder pins it.
//
// The RGB values are for previews and PNG output only; they are never sent to
// the controller. The chromatic four are the vendor's SATURATED_PALETTE, which
// is Pimoroni's rendition of what this panel actually shows — dull, and in the
// blue's case close to navy, because that is what Spectra 6 looks like next to
// a screen. Black and white are kept pure rather than the vendor's (0,0,0) and
// (161,164,165): the latter is paper under somebody's lighting, and a preview
// on a white page reads better with white paper. Not yet measured against our
// own panel.
var Palette = epaper.Palette{
	{Ink: epaper.Black, RGB: color.RGBA{0, 0, 0, 255}},
	{Ink: epaper.White, RGB: color.RGBA{255, 255, 255, 255}},
	{Ink: epaper.Yellow, RGB: color.RGBA{208, 190, 71, 255}},
	{Ink: epaper.Red, RGB: color.RGBA{156, 72, 75, 255}},
	{Ink: epaper.Blue, RGB: color.RGBA{61, 59, 94, 255}},
	{Ink: epaper.Green, RGB: color.RGBA{58, 91, 70, 255}},
}

// wireValue maps a palette index to the four-bit code the controller expects.
//
// From inky_e640.py: BLACK = 0, WHITE = 1, YELLOW = 2, RED = 3, BLUE = 5,
// GREEN = 6, and set_image()'s "remap = numpy.array([0, 1, 2, 3, 5, 6])" with
// the comment "missing colour 4". What 4 does on the glass is not documented,
// and this driver never sends it.
var wireValue = [...]byte{0, 1, 2, 3, 5, 6}

// Controller command codes, with the vendor's names for them (EL640_*).
const (
	cmdPSR   = 0x00 // Panel Setting
	cmdPWR   = 0x01 // Power Setting
	cmdPOF   = 0x02 // Power OFF
	cmdPOFS  = 0x03 // Power OFF Sequence
	cmdPON   = 0x04 // Power ON
	cmdBTST1 = 0x05 // Booster Soft Start 1
	cmdBTST2 = 0x06 // Booster Soft Start 2 — written twice, see refresh
	cmdBTST3 = 0x08 // Booster Soft Start 3
	cmdDTM   = 0x10 // Data Transmission 1 — the framebuffer
	cmdDRF   = 0x12 // Display Refresh — this is the long one
	cmdPLL   = 0x30 // PLL Control
	cmdCDI   = 0x50 // VCOM and Data Interval
	cmdTCON  = 0x60 // TCON Setting
	cmdTRES  = 0x61 // Resolution Setting
	cmdVDCS  = 0x82 // VCOM DC Setting
	cmdPWS   = 0xE3 // Power Saving

	// cmdCMDH is sent first in setup() with a six-byte payload. The vendor
	// gives it no name; 0xAA is conventionally a "command header" unlock on
	// controllers of this family, which is a guess, so the name is ours and
	// says nothing about what it does.
	cmdCMDH = 0xAA
)

// The wire format: four bits per pixel, two pixels to a byte, high nibble
// first. From Inky.show(): ((buf[::2] << 4) & 0xF0) | (buf[1::2] & 0x0F).
const (
	bitsPerPixel  = 4
	pixelsPerByte = 8 / bitsPerPixel
)

// Conn is the transport a driver talks through. It is the same shape as the
// other drivers' Conn, declared here so this package depends on nothing but
// the interface it uses.
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

// Defaults for [Config].
const (
	// DefaultBusyTimeout bounds a single wait. 40 s is the vendor's timeout
	// for the refresh.
	DefaultBusyTimeout = 40 * time.Second

	// DefaultMinRefreshTime is the shortest believable full refresh. A
	// disconnected BUSY line reads ready, so a "refresh" that completes
	// faster than this drew nothing. See the jd79661 driver for the full
	// argument; it applies unchanged.
	DefaultMinRefreshTime = time.Second

	// DefaultCommandDelay is the pause before each command byte: none. The
	// vendor sleeps 300 ms before every command here too — the same line,
	// character for character, as in its JD drivers — and PLAN §9.3 is why
	// this module does not.
	DefaultCommandDelay = 0

	// VendorCommandDelay is the pause the reference implementation uses.
	VendorCommandDelay = 300 * time.Millisecond
)

// Config describes a panel built around this controller.
type Config struct {
	// Width and Height are the panel geometry in pixels, as the picture is
	// presented: 600x400 for the Inky Impression 4.0". Height must be even,
	// because it is the controller's fast axis and two pixels share a byte.
	//
	// Note that the board's EEPROM records the controller's portrait
	// 400x600, not this. The board package swaps them.
	Width, Height int

	// Model is the name reported by [Device.Model]. Boards should pass the
	// name from their EEPROM; it defaults to the controller name.
	Model string

	// CommandDelay is the pause before each controller command. Zero or
	// less means none, which is [DefaultCommandDelay].
	CommandDelay time.Duration

	// BusyTimeout bounds a single wait for the panel. Zero means
	// [DefaultBusyTimeout].
	BusyTimeout time.Duration

	// MinRefreshTime is the shortest refresh treated as believable. Zero
	// means [DefaultMinRefreshTime]; a negative value disables the check,
	// which is what a test with a fake transport wants.
	MinRefreshTime time.Duration
}

// Device is a panel driven by an E640.
type Device struct {
	conn Conn
	cfg  Config

	bounds  image.Rectangle
	palette epaper.Palette

	// The controller's frame: ctrlW is the fast axis — the panel's height —
	// and ctrlH the slow one. See [Device.Show].
	ctrlW, ctrlH int

	// mu serialises Show; see PLAN §9.6.
	mu     sync.Mutex
	closed bool
}

// ErrBusyNotConnected means the panel reported a refresh finished far too
// quickly to be true, which on this hardware means the BUSY line is not
// actually connected. Match with [errors.Is].
var ErrBusyNotConnected = errors.New("e640: BUSY line appears disconnected")

// New returns a driver for a panel of the given geometry.
//
// It does not touch the hardware: the panel is reset and initialised at the
// start of every [Device.Show], as the vendor does.
func New(conn Conn, cfg Config) (*Device, error) {
	if conn == nil {
		return nil, fmt.Errorf("e640: conn is nil")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, fmt.Errorf("e640: panel geometry %dx%d is not usable", cfg.Width, cfg.Height)
	}
	if cfg.Height%pixelsPerByte != 0 {
		return nil, fmt.Errorf("e640: panel height %d is odd; it is the controller's fast axis "+
			"and two pixels share a byte, so it must be even", cfg.Height)
	}
	if cfg.Model == "" {
		cfg.Model = "E640"
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
		ctrlW:   cfg.Height,
		ctrlH:   cfg.Width,
	}, nil
}

// Model returns the panel's name.
func (d *Device) Model() string { return d.cfg.Model }

// Bounds returns the panel geometry, in the orientation the picture is drawn
// in: 600x400 landscape, not the controller's 400x600.
func (d *Device) Bounds() image.Rectangle { return d.bounds }

// Palette returns the panel's inks. See [Palette] for why their positions are
// not the wire values.
func (d *Device) Palette() epaper.Palette { return d.palette }

// NewImage returns a blank image of the right size with the right palette.
func (d *Device) NewImage() *image.Paletted {
	return image.NewPaletted(d.bounds, d.palette.Colors())
}

// Show draws an image and returns once the refresh is complete.
//
// Cancelling ctx abandons the wait, not the refresh: the controller has no
// abort. The image is validated before anything reaches the hardware. Safe
// for concurrent use; concurrent callers queue.
//
// # The frame this sends
//
// The picture is landscape and the controller's frame is portrait. Inky.show()
// in inky_e640.py turns it with numpy.rot90(region, -1) and flattens it, which
// index by index is
//
//	controller (cx, cy)  <-  image (x = cy, y = H-1-cx)
//
// the JD79661's rule without its padding. Each pixel is then translated to its
// wire value and packed two to a byte. 240,000 pixels, 120,000 bytes, all of
// them visible.
func (d *Device) Show(ctx context.Context, img *image.Paletted) error {
	if img == nil {
		return fmt.Errorf("e640: show: image is nil: %w", epaper.ErrBadImage)
	}
	if img.Bounds() != d.bounds {
		return fmt.Errorf("e640: show: image is %v but the panel is %v: %w",
			img.Bounds(), d.bounds, epaper.ErrWrongSize)
	}
	if !d.samePalette(img) {
		return fmt.Errorf("e640: show: image palette is not the panel's "+
			"(use Device.NewImage or render.NewCanvasFor): %w", epaper.ErrPaletteMismatch)
	}

	// Pack before taking the lock and before touching the panel: a bad
	// image should fail without leaving the controller half-initialised.
	frame, err := d.frame(img)
	if err != nil {
		return fmt.Errorf("e640: show: %w", err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return fmt.Errorf("e640: show: %w", epaper.ErrClosed)
	}
	return d.refresh(ctx, frame)
}

// frame converts the picture into the controller's framebuffer. See
// [Device.Show] for the mapping and where it comes from.
func (d *Device) frame(img *image.Paletted) ([]byte, error) {
	h := d.cfg.Height
	limit := uint8(len(wireValue))
	out := make([]byte, d.ctrlW*d.ctrlH/pixelsPerByte)

	n := 0
	for cy := range d.ctrlH {
		// cy is the image's x, so this walks down a column of the image.
		// At 240,000 pixels against a refresh measured in tens of seconds,
		// the stride is not worth reorganising for.
		for cx := 0; cx < d.ctrlW; cx += pixelsPerByte {
			hi := img.Pix[img.PixOffset(cy, h-1-cx)]
			lo := img.Pix[img.PixOffset(cy, h-2-cx)]
			if hi >= limit || lo >= limit {
				bad, y := hi, h-1-cx
				if hi < limit {
					bad, y = lo, h-2-cx
				}
				return nil, fmt.Errorf("pixel (%d,%d) has index %d but the palette defines %d colours: %w",
					cy, y, bad, limit, epaper.ErrBadImage)
			}
			out[n] = wireValue[hi]<<bitsPerPixel | wireValue[lo]
			n++
		}
	}
	return out, nil
}

// refresh runs the full init-and-draw sequence, from Inky._update().
func (d *Device) refresh(ctx context.Context, frame []byte) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("e640: show: %w", err)
	}
	if err := d.init(ctx); err != nil {
		return err
	}

	if err := d.command(cmdDTM, frame); err != nil {
		return fmt.Errorf("e640: sending framebuffer: %w", err)
	}
	if err := d.command(cmdPON, nil); err != nil {
		return fmt.Errorf("e640: power on: %w", err)
	}
	if err := d.conn.WaitReady(ctx, d.cfg.BusyTimeout); err != nil {
		return fmt.Errorf("e640: waiting after power on: %w", err)
	}

	// The booster, again, with a different last byte from init's 0x17. The
	// vendor's only comment is "second setting of the BTST2 register".
	// TestBoosterIsReprogrammedAfterPowerOn stops it being tidied away.
	if err := d.command(cmdBTST2, []byte{0x6F, 0x1F, 0x17, 0x47}); err != nil {
		return fmt.Errorf("e640: booster: %w", err)
	}

	if err := d.command(cmdDRF, []byte{0x00}); err != nil {
		return fmt.Errorf("e640: refresh: %w", err)
	}
	started := time.Now()
	if err := d.conn.WaitReady(ctx, d.cfg.BusyTimeout); err != nil {
		return fmt.Errorf("e640: waiting after refresh: %w", err)
	}
	if err := d.checkRefreshWasReal(time.Since(started)); err != nil {
		return err
	}

	// Power off, and that is the end: the vendor sends no deep sleep, and
	// the next Show resets the controller regardless.
	if err := d.command(cmdPOF, []byte{0x00}); err != nil {
		return fmt.Errorf("e640: power off: %w", err)
	}
	if err := d.conn.WaitReady(ctx, d.cfg.BusyTimeout); err != nil {
		return fmt.Errorf("e640: waiting after power off: %w", err)
	}
	return nil
}

func (d *Device) checkRefreshWasReal(elapsed time.Duration) error {
	if d.cfg.MinRefreshTime < 0 || elapsed >= d.cfg.MinRefreshTime {
		return nil
	}
	return fmt.Errorf("e640: the panel reported the refresh complete after only %s, "+
		"but a full refresh takes several seconds. The BUSY line reads ready when it is "+
		"disconnected, so check its wiring — nothing was drawn: %w", elapsed, ErrBusyNotConnected)
}

// init resets the panel and sends the initialisation sequence, verbatim from
// Inky.setup() in inky_e640.py, in its order.
//
// Unlike the JD drivers, this waits on BUSY straight after the reset pulse,
// because the vendor does — setup() calls _busy_wait(0.3) there. The vendor's
// version sleeps the full 0.3 s if BUSY is already high; ours returns as soon
// as the panel says it is ready, and whether that shortcut is safe is a
// question for the bench, not for this comment. See PLAN §14.
func (d *Device) init(ctx context.Context) error {
	if err := d.conn.Reset(ctx); err != nil {
		return fmt.Errorf("e640: reset: %w", err)
	}
	if err := d.conn.WaitReady(ctx, d.cfg.BusyTimeout); err != nil {
		return fmt.Errorf("e640: waiting after reset: %w", err)
	}

	for _, c := range []struct {
		cmd  byte
		data []byte
	}{
		{cmdCMDH, []byte{0x49, 0x55, 0x20, 0x08, 0x09, 0x18}},
		{cmdPWR, []byte{0x3F}},
		{cmdPSR, []byte{0x5F, 0x69}},
		{cmdBTST1, []byte{0x40, 0x1F, 0x1F, 0x2C}},
		{cmdBTST3, []byte{0x6F, 0x1F, 0x1F, 0x22}},
		{cmdBTST2, []byte{0x6F, 0x1F, 0x17, 0x17}},
		{cmdPOFS, []byte{0x00, 0x54, 0x00, 0x44}},
		{cmdTCON, []byte{0x02, 0x00}},
		{cmdPLL, []byte{0x08}},
		{cmdCDI, []byte{0x3F}},
		// Resolution, as two big-endian 16-bit values: the controller's
		// portrait frame, short axis first. The vendor hardcodes
		// 0x01,0x90,0x02,0x58, which is 400 then 600.
		{cmdTRES, []byte{byte(d.ctrlW >> 8), byte(d.ctrlW), byte(d.ctrlH >> 8), byte(d.ctrlH)}},
		{cmdPWS, []byte{0x2F}},
		{cmdVDCS, []byte{0x01}},
	} {
		if err := d.command(c.cmd, c.data); err != nil {
			return fmt.Errorf("e640: init command 0x%02X: %w", c.cmd, err)
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

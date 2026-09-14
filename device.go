package epaper

import (
	"context"
	"errors"
	"image"
)

// Sentinel errors. Callers match with [errors.Is]; every error this library
// returns wraps one of these, with context added by the layer that found the
// problem.
//
// Nothing here calls os.Exit and nothing panics on hardware state. A library
// that kills its caller cannot be used in a service, which is exactly what
// this one is for.
var (
	// ErrBadPalette means a palette is unusable: empty, or naming the same
	// ink twice. It indicates a driver bug, not a caller mistake.
	ErrBadPalette = errors.New("epaper: invalid palette")

	// ErrPaletteTooLarge means an image has more colours than the panel's
	// wire format can address — more than four for a 2-bit controller.
	ErrPaletteTooLarge = errors.New("epaper: palette too large for this panel")

	// ErrBadImage means an image cannot be sent: nil, empty, or holding a
	// pixel index that its own palette does not define.
	ErrBadImage = errors.New("epaper: invalid image")

	// ErrPaletteMismatch means the image's palette is not the device's.
	// Sending it anyway would have the controller interpret the indices
	// against a different colour order and quietly draw the wrong picture.
	ErrPaletteMismatch = errors.New("epaper: image palette does not match the device")

	// ErrWrongSize means an image's bounds are not the panel's bounds.
	ErrWrongSize = errors.New("epaper: image is not the size of the panel")

	// ErrClosed means the device has already been closed.
	ErrClosed = errors.New("epaper: device is closed")
)

// Device is a panel.
//
// Implementations are not safe for concurrent use unless they say so; the
// bundled ones serialise [Device.Show] internally, so concurrent callers queue
// rather than interleave a framebuffer.
type Device interface {
	// Model is the panel's name, as reported by the hardware. It is free
	// text rather than an enum: it comes off an EEPROM, and the list of
	// possible values is not ours to close.
	Model() string

	// Bounds is the panel's pixel geometry, always anchored at (0,0).
	Bounds() image.Rectangle

	// Palette is what this panel can display. Its order is the controller's
	// index order — see [Palette].
	Palette() Palette

	// NewImage returns a blank image of the right size with the right
	// palette, so the two can never disagree with the device.
	NewImage() *image.Paletted

	// Show draws an image and returns once the refresh is complete, which on
	// a typical four-colour panel takes around 25 seconds.
	//
	// Cancelling ctx abandons the wait, not the refresh: the panel has no
	// abort, so it finishes redrawing regardless and the next Show will
	// block until it has. This is surprising, which is why it is written
	// down here.
	//
	// The image must match the device's bounds and palette, or Show returns
	// [ErrWrongSize] or [ErrPaletteMismatch] without touching the hardware.
	Show(ctx context.Context, img *image.Paletted) error

	// Close releases the hardware. It is safe to call more than once.
	Close() error
}

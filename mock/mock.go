// Package mock provides an in-memory [epaper.Device].
//
// It exists so that a display program can be built and tested with no hardware
// in the room: the mock records every frame it is shown, hands them back for
// inspection, and can be told to fail on demand so error paths get exercised
// too. The bench work that motivated this library found four layout bugs in
// roughly fifty milliseconds each this way; finding them on the panel would
// have cost 20 seconds a go, plus a walk to the other room.
package mock

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"sync"

	"github.com/sweeney/epaper"
)

// Device is an in-memory panel. The zero value is not usable; call [New] or
// [NewLike].
//
// Unlike most test doubles it validates as strictly as the real driver does:
// an image of the wrong size or with the wrong palette is rejected with the
// same error a real panel would give. A mock that accepted anything would let
// bugs through to the hardware, which is the one place they are expensive.
//
// Safe for concurrent use.
type Device struct {
	model   string
	bounds  image.Rectangle
	palette epaper.Palette

	mu       sync.Mutex
	frames   []*image.Paletted
	failNext error
	closed   bool
}

// New returns a mock panel of the given size and palette.
func New(w, h int, p epaper.Palette) *Device {
	return &Device{
		model:   fmt.Sprintf("mock %dx%d", w, h),
		bounds:  image.Rect(0, 0, w, h),
		palette: p,
	}
}

// NewLike returns a mock with the same geometry and palette as an existing
// device, so a test can stand in for a specific panel without repeating its
// dimensions.
func NewLike(d epaper.Device) *Device {
	b := d.Bounds()
	m := New(b.Dx(), b.Dy(), d.Palette())
	m.model = "mock of " + d.Model()
	return m
}

// Model returns the mock's name, which names the panel it stands in for when
// built with [NewLike].
func (d *Device) Model() string { return d.model }

// Bounds returns the panel geometry.
func (d *Device) Bounds() image.Rectangle { return d.bounds }

// Palette returns the panel's inks.
func (d *Device) Palette() epaper.Palette { return d.palette }

// NewImage returns a blank image of the right size with the right palette.
func (d *Device) NewImage() *image.Paletted {
	return image.NewPaletted(d.bounds, d.palette.Colors())
}

// Show records a copy of the image.
//
// It validates exactly as a real driver does, so a test that passes here will
// not fail on the panel for a reason the mock could have caught. A cancelled
// context is reported without recording anything.
//
// The frame is copied: real hardware has consumed the pixels by the time Show
// returns, so a caller that keeps drawing into the same image must not find
// its recorded history rewritten.
func (d *Device) Show(ctx context.Context, img *image.Paletted) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return fmt.Errorf("mock: show: %w", epaper.ErrClosed)
	}
	if failure := d.failNext; failure != nil {
		d.failNext = nil
		return failure
	}
	if img == nil {
		return fmt.Errorf("mock: show: image is nil: %w", epaper.ErrBadImage)
	}
	if img.Bounds() != d.bounds {
		return fmt.Errorf("mock: show: image is %v but the panel is %v: %w",
			img.Bounds(), d.bounds, epaper.ErrWrongSize)
	}
	if !samePalette(img, d.palette) {
		return fmt.Errorf("mock: show: image palette is not the panel's "+
			"(use Device.NewImage or render.NewCanvasFor): %w", epaper.ErrPaletteMismatch)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("mock: show: %w", err)
	}

	clone := image.NewPaletted(img.Bounds(), img.Palette)
	copy(clone.Pix, img.Pix)
	d.frames = append(d.frames, clone)
	return nil
}

// Close marks the device closed. Subsequent calls to [Device.Show] report
// [epaper.ErrClosed]. It is safe to call more than once.
func (d *Device) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	return nil
}

// Frames returns every image the device has been shown, oldest first. The
// slice is a copy; the images in it are not, and should be treated as
// read-only.
func (d *Device) Frames() []*image.Paletted {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]*image.Paletted, len(d.frames))
	copy(out, d.frames)
	return out
}

// Last returns the most recent frame, or nil if the device has not been shown
// anything.
func (d *Device) Last() *image.Paletted {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.frames) == 0 {
		return nil
	}
	return d.frames[len(d.frames)-1]
}

// FailNextShow makes the next call to [Device.Show] return err without
// recording a frame. Later calls succeed as usual.
//
// This is for exercising a consumer's error handling, which on a display that
// nobody is watching is the path most likely to be wrong.
func (d *Device) FailNextShow(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.failNext = err
}

// SavePNG writes the most recent frame to a file, using the palette's RGB
// values. It reports an error if nothing has been shown yet.
func (d *Device) SavePNG(path string) error {
	last := d.Last()
	if last == nil {
		return errors.New("mock: save png: nothing has been shown yet")
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("mock: save png: %w", err)
	}
	if err := png.Encode(f, last); err != nil {
		return fmt.Errorf("mock: save png: %w", errors.Join(err, f.Close()))
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("mock: save png: %w", err)
	}
	return nil
}

// samePalette reports whether an image's colours are the device's, in the same
// order. Order matters: the indices mean different colours otherwise.
func samePalette(img *image.Paletted, p epaper.Palette) bool {
	if len(img.Palette) != len(p) {
		return false
	}
	for i, e := range p {
		if img.Palette[i] != color.Color(e.RGB) {
			return false
		}
	}
	return true
}

package mock_test

import (
	"context"
	"errors"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/mock"
)

var fourInk = epaper.Palette{
	{Ink: epaper.Black, RGB: color.RGBA{0, 0, 0, 255}},
	{Ink: epaper.White, RGB: color.RGBA{255, 255, 255, 255}},
	{Ink: epaper.Yellow, RGB: color.RGBA{235, 205, 40, 255}},
	{Ink: epaper.Red, RGB: color.RGBA{190, 45, 40, 255}},
}

// The mock is only useful if it is substitutable for the real thing.
var _ epaper.Device = (*mock.Device)(nil)

func TestDeviceShape(t *testing.T) {
	d := mock.New(400, 300, fourInk)
	defer d.Close()

	if got, want := d.Bounds(), image.Rect(0, 0, 400, 300); got != want {
		t.Errorf("Bounds() = %v, want %v", got, want)
	}
	if d.Model() == "" {
		t.Error("Model() is empty")
	}
	img := d.NewImage()
	if got := img.Bounds(); got != d.Bounds() {
		t.Errorf("NewImage() bounds = %v, want %v", got, d.Bounds())
	}
	if len(img.Palette) != len(fourInk) {
		t.Errorf("NewImage() palette has %d colours, want %d", len(img.Palette), len(fourInk))
	}
}

func TestShowRecordsFrames(t *testing.T) {
	d := mock.New(4, 2, fourInk)
	defer d.Close()
	ctx := context.Background()

	if got := d.Frames(); len(got) != 0 {
		t.Fatalf("Frames() = %d before any Show, want 0", len(got))
	}
	if d.Last() != nil {
		t.Fatal("Last() is non-nil before any Show")
	}

	for i := range 3 {
		img := d.NewImage()
		img.Pix[0] = uint8(i)
		if err := d.Show(ctx, img); err != nil {
			t.Fatalf("Show() error: %v", err)
		}
	}

	frames := d.Frames()
	if len(frames) != 3 {
		t.Fatalf("Frames() = %d, want 3", len(frames))
	}
	for i, f := range frames {
		if got := f.Pix[0]; got != uint8(i) {
			t.Errorf("frame %d pixel 0 = %d, want %d", i, got, i)
		}
	}
	if d.Last().Pix[0] != 2 {
		t.Errorf("Last() pixel 0 = %d, want 2", d.Last().Pix[0])
	}
}

// A consumer that draws into one image and shows it repeatedly must not find
// its history rewritten. The real hardware has already consumed the pixels by
// the time Show returns; the mock has to behave the same way.
func TestShowCopiesTheFrame(t *testing.T) {
	d := mock.New(4, 2, fourInk)
	defer d.Close()

	img := d.NewImage()
	img.Pix[0] = 1
	if err := d.Show(context.Background(), img); err != nil {
		t.Fatalf("Show() error: %v", err)
	}

	img.Pix[0] = 3 // caller keeps drawing after the show
	if got := d.Last().Pix[0]; got != 1 {
		t.Errorf("recorded frame pixel 0 = %d, want 1 — the mock kept a reference, not a copy", got)
	}
}

// Frames() must not hand out the internal slice either.
func TestFramesIsACopy(t *testing.T) {
	d := mock.New(4, 2, fourInk)
	defer d.Close()
	if err := d.Show(context.Background(), d.NewImage()); err != nil {
		t.Fatalf("Show() error: %v", err)
	}

	got := d.Frames()
	got[0] = nil
	if d.Frames()[0] == nil {
		t.Error("mutating the result of Frames() changed the recorded history")
	}
}

func TestShowRejects(t *testing.T) {
	d := mock.New(4, 2, fourInk)
	defer d.Close()

	wrongSize := image.NewPaletted(image.Rect(0, 0, 8, 2), fourInk.Colors())
	otherPalette := image.NewPaletted(image.Rect(0, 0, 4, 2), color.Palette{
		color.RGBA{0, 0, 0, 255}, color.RGBA{255, 255, 255, 255},
	})

	for _, tc := range []struct {
		name string
		img  *image.Paletted
		want error
	}{
		{"nil image", nil, epaper.ErrBadImage},
		{"wrong size", wrongSize, epaper.ErrWrongSize},
		{"different palette", otherPalette, epaper.ErrPaletteMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := d.Show(context.Background(), tc.img); !errors.Is(err, tc.want) {
				t.Errorf("Show() error = %v, want %v", err, tc.want)
			}
		})
	}
	if got := len(d.Frames()); got != 0 {
		t.Errorf("Frames() = %d after only rejected shows, want 0", got)
	}
}

func TestShowHonoursContext(t *testing.T) {
	d := mock.New(4, 2, fourInk)
	defer d.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := d.Show(ctx, d.NewImage()); !errors.Is(err, context.Canceled) {
		t.Errorf("Show() error = %v, want context.Canceled", err)
	}
	if got := len(d.Frames()); got != 0 {
		t.Errorf("Frames() = %d after a cancelled Show, want 0", got)
	}
}

func TestFailNextShow(t *testing.T) {
	d := mock.New(4, 2, fourInk)
	defer d.Close()
	ctx := context.Background()

	boom := errors.New("panel on fire")
	d.FailNextShow(boom)

	if err := d.Show(ctx, d.NewImage()); !errors.Is(err, boom) {
		t.Errorf("Show() error = %v, want %v", err, boom)
	}
	if got := len(d.Frames()); got != 0 {
		t.Errorf("a failed Show recorded a frame")
	}
	// Only the next one.
	if err := d.Show(ctx, d.NewImage()); err != nil {
		t.Errorf("second Show() error = %v, want nil", err)
	}
	if got := len(d.Frames()); got != 1 {
		t.Errorf("Frames() = %d, want 1", got)
	}
}

func TestClose(t *testing.T) {
	d := mock.New(4, 2, fourInk)
	if err := d.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Errorf("second Close() error = %v, want nil — Close is idempotent", err)
	}
	if err := d.Show(context.Background(), d.NewImage()); !errors.Is(err, epaper.ErrClosed) {
		t.Errorf("Show() after Close error = %v, want %v", err, epaper.ErrClosed)
	}
}

func TestNewLike(t *testing.T) {
	real := mock.New(400, 300, fourInk)
	defer real.Close()
	like := mock.NewLike(real)
	defer like.Close()

	if like.Bounds() != real.Bounds() {
		t.Errorf("NewLike bounds = %v, want %v", like.Bounds(), real.Bounds())
	}
	if len(like.Palette()) != len(real.Palette()) {
		t.Errorf("NewLike palette length = %d, want %d", len(like.Palette()), len(real.Palette()))
	}
	// An image made by one must be showable on the other.
	if err := like.Show(context.Background(), real.NewImage()); err != nil {
		t.Errorf("Show() of the real device's image on NewLike: %v", err)
	}
}

func TestSavePNG(t *testing.T) {
	d := mock.New(4, 2, fourInk)
	defer d.Close()

	path := filepath.Join(t.TempDir(), "out.png")
	if err := d.SavePNG(path); err == nil {
		t.Error("SavePNG() before any Show returned nil, want an error")
	}

	img := d.NewImage()
	for i := range img.Pix {
		img.Pix[i] = uint8(i % 4)
	}
	if err := d.Show(context.Background(), img); err != nil {
		t.Fatalf("Show() error: %v", err)
	}
	if err := d.SavePNG(path); err != nil {
		t.Fatalf("SavePNG() error: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading PNG: %v", err)
	}
	if len(b) == 0 {
		t.Error("SavePNG() wrote an empty file")
	}
}

// PLAN §9.6: Show serialises internally, so a consumer with a ticker and a
// webhook cannot interleave two framebuffers into one picture.
func TestShowIsSafeForConcurrentUse(t *testing.T) {
	d := mock.New(4, 2, fourInk)
	defer d.Close()

	const n = 50
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			img := d.NewImage()
			img.Pix[0] = uint8(i % 4)
			if err := d.Show(context.Background(), img); err != nil {
				t.Errorf("Show() error: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := len(d.Frames()); got != n {
		t.Errorf("Frames() = %d, want %d", got, n)
	}
}

// The length check is the easy half. A palette of the right length but the
// wrong colours means the indices denote different inks, which is exactly the
// silent wrong-picture failure ErrPaletteMismatch exists to prevent.
func TestShowRejectsSameLengthDifferentColours(t *testing.T) {
	d := mock.New(4, 2, fourInk)
	defer d.Close()

	shuffled := fourInk.Colors()
	shuffled[2], shuffled[3] = shuffled[3], shuffled[2] // yellow and red swapped
	img := image.NewPaletted(d.Bounds(), shuffled)

	if err := d.Show(context.Background(), img); !errors.Is(err, epaper.ErrPaletteMismatch) {
		t.Errorf("Show() error = %v, want %v", err, epaper.ErrPaletteMismatch)
	}
}

func TestSavePNGUnwritablePath(t *testing.T) {
	d := mock.New(4, 2, fourInk)
	defer d.Close()
	if err := d.Show(context.Background(), d.NewImage()); err != nil {
		t.Fatalf("Show() error: %v", err)
	}
	// A directory that does not exist, inside a directory that does.
	path := filepath.Join(t.TempDir(), "no-such-dir", "out.png")
	if err := d.SavePNG(path); err == nil {
		t.Error("SavePNG() to an unwritable path returned nil, want an error")
	}
}

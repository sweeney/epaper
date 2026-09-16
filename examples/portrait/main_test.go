package main

import (
	"context"
	"image"
	"testing"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/inky"
	"github.com/sweeney/epaper/mock"
)

// The rotation is the whole of this example, it is four lines, and every
// plausible-looking variant of it is wrong in a way that costs a 20-second
// refresh to discover. So it is pinned by corner, which is the only thing that
// distinguishes a quarter turn from its three siblings.
func TestRotateCWMovesTheCorners(t *testing.T) {
	// A 2x3 portrait image with every pixel distinct.
	//
	//	A B        turned clockwise      E C A
	//	C D        becomes               F D B
	//	E F
	src := image.NewPaletted(image.Rect(0, 0, 2, 3), panelPalette.Colors())
	copy(src.Pix, []uint8{0, 1, 2, 3, 1, 2}) // A B C D E F, row-major
	const (
		A, B = 0, 1
		C, D = 2, 3
		E, F = 1, 2
	)

	dst := rotateCW(src, panelPalette)

	if got, want := dst.Bounds(), image.Rect(0, 0, 3, 2); got != want {
		t.Fatalf("bounds = %v, want %v — the axes did not swap", got, want)
	}
	for _, tc := range []struct {
		x, y int
		want uint8
		name string
	}{
		{0, 0, E, "src bottom-left -> dst top-left"},
		{2, 0, A, "src top-left -> dst top-RIGHT"},
		{0, 1, F, "src bottom-right -> dst bottom-left"},
		{2, 1, B, "src top-right -> dst bottom-right"},
		{1, 0, C, "middle row"},
		{1, 1, D, "middle row"},
	} {
		if got := dst.ColorIndexAt(tc.x, tc.y); got != tc.want {
			t.Errorf("dst(%d,%d) = %d, want %d — %s", tc.x, tc.y, got, tc.want, tc.name)
		}
	}
}

// Four quarter turns is the identity. This catches a rotation that is
// self-consistent but mirrored, which the corner test above could in principle
// miss and which looks completely normal on a panel until you read the text.
func TestFourRotationsReturnTheOriginal(t *testing.T) {
	src := image.NewPaletted(image.Rect(0, 0, 250, 122), panelPalette.Colors())
	for i := range src.Pix {
		src.Pix[i] = uint8((i*7 + i/250*3) % 4)
	}

	got := src
	for range 4 {
		got = rotateCW(got, panelPalette)
	}
	if got.Bounds() != src.Bounds() {
		t.Fatalf("bounds after four turns = %v, want %v", got.Bounds(), src.Bounds())
	}
	if string(got.Pix) != string(src.Pix) {
		t.Error("four quarter turns did not return the original; the rotation is mirrored")
	}
}

// The rotated image must be one the device will actually accept. That is the
// entire point of the exercise — an unrotated portrait image is rejected with
// ErrWrongSize, and this proves the rotation fixes exactly that.
func TestRotatedImageIsAcceptedAndUnrotatedIsNot(t *testing.T) {
	for _, p := range inky.SupportedPanels() {
		t.Run(p.Model, func(t *testing.T) {
			dev := mock.New(p.Width, p.Height, panelPalette)
			defer func() { _ = dev.Close() }()

			portrait := image.NewPaletted(image.Rect(0, 0, p.Height, p.Width), panelPalette.Colors())

			// Unrotated: refused, and nothing reaches the panel.
			err := dev.Show(context.Background(), portrait)
			if err == nil {
				t.Fatal("Show() accepted a transposed image; this example would be pointless")
			}
			if len(dev.Frames()) != 0 {
				t.Error("a refused image still reached the panel")
			}

			// Rotated: accepted.
			if err := dev.Show(context.Background(), rotateCW(portrait, panelPalette)); err != nil {
				t.Errorf("Show(rotated) = %v, want nil", err)
			}
		})
	}
}

// And the example end to end, on every panel the library drives.
func TestDrawPortraitOnEverySupportedPanel(t *testing.T) {
	for _, p := range inky.SupportedPanels() {
		t.Run(p.Model, func(t *testing.T) {
			dev := mock.New(p.Width, p.Height, panelPalette)
			defer func() { _ = dev.Close() }()

			if err := drawPortrait(context.Background(), dev); err != nil {
				t.Fatalf("drawPortrait(): %v", err)
			}
			last := dev.Last()
			if last == nil {
				t.Fatal("nothing was shown")
			}
			if got, want := last.Bounds(), image.Rect(0, 0, p.Width, p.Height); got != want {
				t.Errorf("shown image is %v, want the panel's %v", got, want)
			}
		})
	}
}

// The palette must survive the rotation. Losing it would fail as
// ErrPaletteMismatch much later, somewhere far less informative.
func TestRotationKeepsThePalette(t *testing.T) {
	src := image.NewPaletted(image.Rect(0, 0, 4, 8), panelPalette.Colors())
	dst := rotateCW(src, panelPalette)
	if len(dst.Palette) != len(panelPalette) {
		t.Fatalf("palette has %d entries, want %d", len(dst.Palette), len(panelPalette))
	}
	for i, e := range panelPalette {
		if dst.Palette[i] != epaper.Palette(panelPalette)[i].RGB {
			t.Errorf("palette[%d] = %v, want %v", i, dst.Palette[i], e.RGB)
		}
	}
}

package main

import (
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/sweeney/epaper"
)

// Both patterns must draw a full, valid frame. This is the binary people reach
// for first, so a break here is the worst possible first impression.
func TestDrawPatterns(t *testing.T) {
	for _, pattern := range []string{"testcard", "conformance"} {
		t.Run(pattern, func(t *testing.T) {
			img, err := draw(bounds400x300(), paletteForPNG(), pattern, "Test Model", "a note")
			if err != nil {
				t.Fatalf("draw(): %v", err)
			}
			if got := img.Bounds(); got != image.Rect(0, 0, 400, 300) {
				t.Errorf("bounds = %v, want 400x300", got)
			}

			// It must pack to exactly what the panel expects.
			frame, err := epaper.Pack(img)
			if err != nil {
				t.Fatalf("Pack(): %v", err)
			}
			if len(frame) != 30000 {
				t.Errorf("packed to %d bytes, want 30000", len(frame))
			}

			// And use every ink, or it is not testing the panel.
			seen := map[uint8]bool{}
			for _, p := range img.Pix {
				seen[p] = true
			}
			if len(seen) != 4 {
				t.Errorf("used %d inks, want 4", len(seen))
			}
		})
	}
}

func TestDrawUnknownPatternFallsBackToTheCard(t *testing.T) {
	// The flag is validated in run(); draw treats anything else as the card
	// rather than returning an empty image.
	img, err := draw(bounds400x300(), paletteForPNG(), "nonsense", "m", "")
	if err != nil {
		t.Fatalf("draw(): %v", err)
	}
	blank := true
	for _, p := range img.Pix {
		if p != 0 {
			blank = false
			break
		}
	}
	if blank {
		t.Error("draw() with an unknown pattern produced a blank image")
	}
}

func TestWritePNG(t *testing.T) {
	img, err := draw(bounds400x300(), paletteForPNG(), "testcard", "m", "")
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "card.png")
	if err := writePNG(path, img); err != nil {
		t.Fatalf("writePNG(): %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() == 0 {
		t.Error("writePNG() wrote an empty file")
	}

	if err := writePNG(filepath.Join(t.TempDir(), "no-such-dir", "x.png"), img); err == nil {
		t.Error("writePNG() to an unwritable path = nil error")
	}
}

// -vendor-timing must actually reach the driver, or the escape hatch for
// PLAN §9.3 is decorative.
func TestCommandDelayFlag(t *testing.T) {
	if got := commandDelay(false); got != 0 {
		t.Errorf("commandDelay(false) = %v, want 0 (the driver default)", got)
	}
	if got := commandDelay(true); got <= 0 {
		t.Errorf("commandDelay(true) = %v, want the vendor's delay", got)
	}
}

func TestPaletteForPNGMatchesTheDriver(t *testing.T) {
	p := paletteForPNG()
	if len(p) != 4 {
		t.Fatalf("palette has %d entries, want 4", len(p))
	}
	// Order is the wire format; getting it wrong here would make the offline
	// PNG disagree with the panel.
	for i, want := range []epaper.Ink{epaper.Black, epaper.White, epaper.Yellow, epaper.Red} {
		if p[i].Ink != want {
			t.Errorf("palette[%d] = %s, want %s", i, p[i].Ink, want)
		}
	}
}

package main

import (
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/inky"
)

// bounds400x300 was the command's hardcoded offline geometry before -size
// existed. It stays here, in the test, because these assertions are about the
// 400x300 card specifically — the every-panel coverage is TestDrawEverySupportedPanel.
func bounds400x300() image.Rectangle { return image.Rect(0, 0, 400, 300) }

// Both patterns must draw a full, valid frame. This is the binary people reach
// for first, so a break here is the worst possible first impression.
func TestDrawPatterns(t *testing.T) {
	for _, pattern := range []string{"testcard", "orientation", "conformance"} {
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

	// A missing directory is created — "-png out/card.png -size all" is the
	// natural way to use this and should not need a mkdir first.
	nested := filepath.Join(t.TempDir(), "a", "b", "card.png")
	if err := writePNG(nested, img); err != nil {
		t.Errorf("writePNG() to a missing directory = %v, want it created", err)
	}

	// But a genuinely impossible path must still fail rather than be
	// swallowed: here the "directory" is an existing regular file.
	file := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writePNG(filepath.Join(file, "x.png"), img); err == nil {
		t.Error("writePNG() under a regular file = nil error")
	}
}

// Every geometry the library supports must render, for every pattern. This is
// the command-level version of the testcard package's golden coverage: the
// binary people actually run has to work on every panel, not just the one the
// author owned.
func TestDrawEverySupportedPanel(t *testing.T) {
	panels := inky.SupportedPanels()
	if len(panels) == 0 {
		t.Fatal("no supported panels")
	}
	for _, p := range panels {
		for _, pattern := range []string{"testcard", "orientation", "conformance"} {
			t.Run(p.Model+"/"+pattern, func(t *testing.T) {
				img, err := draw(image.Rect(0, 0, p.Width, p.Height), paletteForPNG(), pattern, p.Model, "")
				if err != nil {
					t.Fatalf("draw(): %v", err)
				}
				if got := img.Bounds(); got != image.Rect(0, 0, p.Width, p.Height) {
					t.Errorf("bounds = %v, want %dx%d", got, p.Width, p.Height)
				}
			})
		}
	}
}

func TestParseSize(t *testing.T) {
	for _, tc := range []struct {
		in   string
		w, h int
		ok   bool
	}{
		{"250x122", 250, 122, true},
		{"400X300", 400, 300, true},
		{"1600x1200", 1600, 1200, true},
		{"", 0, 0, false},
		{"250", 0, 0, false},
		{"x122", 0, 0, false},
		{"250x", 0, 0, false},
		{"axb", 0, 0, false},
		{"pHAT", 0, 0, false},
	} {
		w, h, ok := parseSize(tc.in)
		if ok != tc.ok || w != tc.w || h != tc.h {
			t.Errorf("parseSize(%q) = %d, %d, %v; want %d, %d, %v", tc.in, w, h, ok, tc.w, tc.h, tc.ok)
		}
	}
}

func TestPanelsFor(t *testing.T) {
	all := inky.SupportedPanels()

	for _, tc := range []struct {
		name  string
		size  string
		want  int
		check func(t *testing.T, got []inky.Panel)
	}{
		{"empty means all", "", len(all), nil},
		{"all means all", "all", len(all), nil},
		{
			"an explicit geometry", "250x122", 1,
			func(t *testing.T, got []inky.Panel) {
				if got[0].Width != 250 || got[0].Height != 122 {
					t.Errorf("got %dx%d", got[0].Width, got[0].Height)
				}
			},
		},
		{
			// A size nobody sells must still render: the flag exists to see
			// what the layout does before a driver is written.
			"an unsold geometry", "600x448", 1,
			func(t *testing.T, got []inky.Panel) {
				if got[0].Width != 600 || got[0].Height != 448 {
					t.Errorf("got %dx%d", got[0].Width, got[0].Height)
				}
			},
		},
		{
			"a model substring", "phat", 1,
			func(t *testing.T, got []inky.Panel) {
				if got[0].Width != 250 {
					t.Errorf("matched %q, want the pHAT", got[0].Model)
				}
			},
		},
		{
			"a controller name", "JD79668", 1,
			func(t *testing.T, got []inky.Panel) {
				if got[0].Controller != "JD79668" {
					t.Errorf("matched %q", got[0].Controller)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := panelsFor(tc.size)
			if err != nil {
				t.Fatalf("panelsFor(%q): %v", tc.size, err)
			}
			if len(got) != tc.want {
				t.Fatalf("panelsFor(%q) returned %d panels, want %d", tc.size, len(got), tc.want)
			}
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}

	for _, bad := range []string{"nonsense", "0x0", "-5x10"} {
		if _, err := panelsFor(bad); err == nil {
			t.Errorf("panelsFor(%q) = nil error, want a failure", bad)
		}
	}
}

func TestSuffixed(t *testing.T) {
	for _, tc := range []struct{ in, suffix, want string }{
		{"card.png", "-250x122", "card-250x122.png"},
		{"out/card.png", "-400x300", "out/card-400x300.png"},
		{"card", "-1x2", "card-1x2"},
	} {
		if got := suffixed(tc.in, tc.suffix); got != tc.want {
			t.Errorf("suffixed(%q, %q) = %q, want %q", tc.in, tc.suffix, got, tc.want)
		}
	}
}

func TestValidPattern(t *testing.T) {
	for _, p := range []string{"testcard", "orientation", "conformance"} {
		if !validPattern(p) {
			t.Errorf("validPattern(%q) = false", p)
		}
	}
	for _, p := range []string{"", "nonsense", "Testcard"} {
		if validPattern(p) {
			t.Errorf("validPattern(%q) = true", p)
		}
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

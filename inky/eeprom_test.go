package inky_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sweeney/epaper/inky"
)

// The fixtures live at the repo root so the conformance and EEPROM sets stay
// together; see testdata/README.md.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "testdata", "eeprom", name))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	return b
}

// The real board, captured 2026-09-14. Every field must decode exactly.
func TestParseEEPROMRealBoard(t *testing.T) {
	got, err := inky.ParseEEPROM(fixture(t, "what-jd79668.bin"))
	if err != nil {
		t.Fatalf("ParseEEPROM() error: %v", err)
	}

	for _, tc := range []struct {
		field     string
		got, want any
	}{
		{"Width", got.Width, 400},
		{"Height", got.Height, 300},
		{"Colour", got.Colour, "red/yellow"},
		{"DisplayVariant", int(got.DisplayVariant), 24},
		{"Model", got.Model, "Red/Yellow wHAT (JD79668)"},
		{"WriteTime", got.WriteTime, "2025-08-20 15:51:55.5"},
		// Stored times ten: the EEPROM holds 100 for board revision 10.0.
		{"PCBVariant (raw)", int(got.PCBVariant), 100},
		{"PCBRevision", got.PCBRevision(), "10.0"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.field, tc.got, tc.want)
		}
	}
}

// The other real board. Captured from the pHAT on 2026-09-16.
//
// This is what pins the display-variant NUMBER, and that matters more than it
// looks: the variant byte is the only thing that tells the two four-ink Inky
// boards apart, and picking the wrong driver produces a blank panel rather
// than an error.
func TestParseRealPHat(t *testing.T) {
	got, err := inky.ParseEEPROM(fixture(t, "phat-jd79661.bin"))
	if err != nil {
		t.Fatalf("ParseEEPROM() error: %v", err)
	}

	for _, tc := range []struct {
		field     string
		got, want any
	}{
		{"Width", got.Width, 250},
		{"Height", got.Height, 122},
		{"Colour", got.Colour, "red/yellow"},
		{"DisplayVariant", int(got.DisplayVariant), 23},
		{"Model", got.Model, "Red/Yellow pHAT (JD79661)"},
		{"WriteTime", got.WriteTime, "2026-04-15 23:32:34.2"},
		{"PCBVariant (raw)", int(got.PCBVariant), 100},
		{"PCBRevision", got.PCBRevision(), "10.0"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.field, tc.got, tc.want)
		}
	}
}

// The two boards must not be confusable. Everything else about them matches —
// same colour string, same PCB revision, same pin map — so the variant byte is
// carrying the whole distinction on its own.
func TestTheTwoRealBoardsDifferOnlyByVariantAndSize(t *testing.T) {
	what, err := inky.ParseEEPROM(fixture(t, "what-jd79668.bin"))
	if err != nil {
		t.Fatalf("ParseEEPROM(wHAT): %v", err)
	}
	phat, err := inky.ParseEEPROM(fixture(t, "phat-jd79661.bin"))
	if err != nil {
		t.Fatalf("ParseEEPROM(pHAT): %v", err)
	}

	if what.DisplayVariant == phat.DisplayVariant {
		t.Fatalf("both boards report display variant %d; nothing can tell them apart", what.DisplayVariant)
	}
	if what.Colour != phat.Colour {
		t.Errorf("colours differ (%q vs %q) — if that ever becomes true, the dispatch could use it",
			what.Colour, phat.Colour)
	}
}

// "red/yellow" means BOTH inks at once, not a choice between them. The board's
// own handover notes said otherwise and were wrong, which cost a day.
func TestRealBoardIsFourInk(t *testing.T) {
	got, err := inky.ParseEEPROM(fixture(t, "what-jd79668.bin"))
	if err != nil {
		t.Fatalf("ParseEEPROM() error: %v", err)
	}
	if got.Colour != "red/yellow" {
		t.Fatalf("Colour = %q, want %q", got.Colour, "red/yellow")
	}
}

func TestParseEEPROMEmptyWriteTime(t *testing.T) {
	got, err := inky.ParseEEPROM(fixture(t, "empty-writetime.bin"))
	if err != nil {
		t.Fatalf("ParseEEPROM() error: %v — a zero-length Pascal string is valid", err)
	}
	if got.WriteTime != "" {
		t.Errorf("WriteTime = %q, want empty", got.WriteTime)
	}
	if got.Width != 400 || got.Height != 300 {
		t.Errorf("geometry = %dx%d, want 400x300", got.Width, got.Height)
	}
}

// Each of these corresponds to a plausible parser bug. See testdata/README.md.
func TestParseEEPROMRejects(t *testing.T) {
	for _, tc := range []struct {
		file string
		why  string
		want error
	}{
		{"truncated.bin", "a short read must not yield a zero-valued struct", inky.ErrBadEEPROM},
		{"zeroed.bin", "no EEPROM present: geometry 0x0 is impossible", inky.ErrBadEEPROM},
		{"ones.bin", "floating bus: geometry 65535x65535 is impossible", inky.ErrBadEEPROM},
		{"zero-geometry.bin", "valid variant but no geometry", inky.ErrBadEEPROM},
		{"unknown-variant.bin", "variant 254 must not index past the table", inky.ErrUnknownVariant},
	} {
		t.Run(tc.file, func(t *testing.T) {
			got, err := inky.ParseEEPROM(fixture(t, tc.file))
			if err == nil {
				t.Fatalf("ParseEEPROM() = %+v, want an error: %s", got, tc.why)
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("ParseEEPROM() error = %v, want %v (%s)", err, tc.want, tc.why)
			}
			if got != nil {
				t.Errorf("ParseEEPROM() returned %+v alongside an error; it must return nil", got)
			}
		})
	}
}

// The unknown-variant error has to name the number, or the person reading the
// log has no idea which board they are holding.
func TestUnknownVariantErrorNamesTheVariant(t *testing.T) {
	_, err := inky.ParseEEPROM(fixture(t, "unknown-variant.bin"))
	if err == nil {
		t.Fatal("ParseEEPROM() = nil error")
	}
	if !contains(err.Error(), "254") {
		t.Errorf("error %q does not mention variant 254", err)
	}
}

func TestParseEEPROMLengths(t *testing.T) {
	full := fixture(t, "what-jd79668.bin")
	for _, tc := range []struct {
		name string
		in   []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"one byte short", full[:len(full)-1]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := inky.ParseEEPROM(tc.in); !errors.Is(err, inky.ErrBadEEPROM) {
				t.Errorf("ParseEEPROM() error = %v, want %v", err, inky.ErrBadEEPROM)
			}
		})
	}

	// A longer read is fine: the I2C layer may hand back a padded buffer.
	if _, err := inky.ParseEEPROM(append(full, 0x00, 0x00)); err != nil {
		t.Errorf("ParseEEPROM() on an over-long buffer: %v, want success", err)
	}
}

// A Pascal length longer than the bytes available is the classic
// index-past-the-end bug. It must be rejected, not clamped silently.
func TestParseEEPROMOverlongPascalString(t *testing.T) {
	b := fixture(t, "what-jd79668.bin")
	b[7] = 22 // only 21 bytes of storage follow
	if _, err := inky.ParseEEPROM(b); !errors.Is(err, inky.ErrBadEEPROM) {
		t.Errorf("ParseEEPROM() error = %v, want %v", err, inky.ErrBadEEPROM)
	}
}

func TestParseEEPROMUnknownColour(t *testing.T) {
	b := fixture(t, "what-jd79668.bin")
	b[4] = 4 // a hole in the vendor's colour table
	if _, err := inky.ParseEEPROM(b); !errors.Is(err, inky.ErrBadEEPROM) {
		t.Errorf("ParseEEPROM() error = %v, want %v", err, inky.ErrBadEEPROM)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

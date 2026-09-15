package inky_test

import (
	"testing"

	"github.com/sweeney/epaper/inky"
)

// FuzzParseEEPROM throws arbitrary bytes at the identification parser.
//
// It is worth fuzzing because it is the one place this library parses data it
// did not produce. The bytes come off an I2C bus, and the bench already proved
// that bus can hand back convincing garbage — a floating line reads as all
// ones, a byte-mode read returns a plausible-looking record that is nothing of
// the sort. A parser that panics on any of that takes the caller's process
// down with it, which PLAN §1.3 forbids outright.
//
// The contract asserted here is narrow and total: never panic, and never
// return a nil error alongside a nil record.
func FuzzParseEEPROM(f *testing.F) {
	// Seed with the real board, then the six failure modes.
	f.Add(fixture(&testing.T{}, "what-jd79668.bin"))
	for _, name := range []string{
		"truncated.bin", "zeroed.bin", "ones.bin",
		"unknown-variant.bin", "zero-geometry.bin", "empty-writetime.bin",
	} {
		f.Add(fixture(&testing.T{}, name))
	}
	f.Add([]byte{})
	f.Add(make([]byte, 29))

	f.Fuzz(func(t *testing.T, b []byte) {
		got, err := inky.ParseEEPROM(b)

		switch {
		case err != nil && got != nil:
			t.Fatalf("ParseEEPROM(% x) returned both a record and an error %v", b, err)
		case err == nil && got == nil:
			t.Fatalf("ParseEEPROM(% x) returned neither a record nor an error", b)
		case err != nil:
			return
		}

		// A successful parse must describe something possible, or a caller
		// will believe it.
		if got.Width <= 0 || got.Height <= 0 {
			t.Errorf("accepted geometry %dx%d", got.Width, got.Height)
		}
		if got.Model == "" {
			t.Error("accepted a record with no model name")
		}
		if got.Colour == "" {
			t.Error("accepted a record with no colour")
		}
		if len(got.WriteTime) > 21 {
			t.Errorf("write time is %d bytes, longer than the field", len(got.WriteTime))
		}
		// PCBRevision does arithmetic on the raw byte; it must not panic or
		// produce nonsense for any of the 256 values.
		if rev := got.PCBRevision(); rev == "" {
			t.Error("PCBRevision() is empty")
		}
	})
}

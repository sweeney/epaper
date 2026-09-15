//go:build linux

package i2c

import (
	"path/filepath"
	"strings"
	"testing"
)

// The bus is opened from a device path, so a missing one has to fail cleanly
// and say which path it tried.
func TestOpenMissingBus(t *testing.T) {
	_, err := Open(filepath.Join(t.TempDir(), "i2c-nope"))
	if err == nil {
		t.Fatal("Open() on a missing bus = nil error")
	}
	if !strings.Contains(err.Error(), "i2c-nope") {
		t.Errorf("error %q does not name the bus it tried", err)
	}
}

// Every method has to cope with a zero or closed Bus rather than dereferencing
// a nil file. The EEPROM read happens during Open, so a panic here takes down
// a program that has not drawn anything yet.
func TestZeroBus(t *testing.T) {
	var b Bus

	if _, err := b.ReadReg16(0x50, 0, 29); err == nil {
		t.Error("ReadReg16() on a closed bus = nil error")
	}
	if err := b.Close(); err != nil {
		t.Errorf("Close() on a never-opened bus = %v", err)
	}
	// Idempotent, because Open's failure paths close what they have.
	if err := b.Close(); err != nil {
		t.Errorf("second Close() = %v", err)
	}
}

// A non-positive length would make the kernel read into a zero-length buffer,
// so it is refused before the ioctl rather than producing an empty record that
// the parser would then reject for the wrong reason.
func TestReadReg16RejectsABadLength(t *testing.T) {
	var b Bus
	for _, n := range []int{0, -1, -29} {
		_, err := b.ReadReg16(0x50, 0, n)
		if err == nil {
			t.Errorf("ReadReg16(n=%d) = nil error", n)
			continue
		}
		if !strings.Contains(err.Error(), "positive") {
			t.Errorf("ReadReg16(n=%d) error = %q, want it to explain the length", n, err)
		}
	}
}

// Closing a bus twice must not error, and a closed bus must stay closed.
func TestCloseIsIdempotent(t *testing.T) {
	// /dev/null is openable everywhere and is not an I2C bus, which is fine:
	// this is testing the file lifecycle, not a transfer.
	b, err := Open("/dev/null")
	if err != nil {
		t.Skipf("cannot open /dev/null: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}
	if err := b.Close(); err != nil {
		t.Errorf("second Close() = %v, want nil", err)
	}
	if _, err := b.ReadReg16(0x50, 0, 29); err == nil {
		t.Error("ReadReg16() after Close = nil error")
	}
}

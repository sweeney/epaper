//go:build linux

package i2c

import (
	"testing"
	"unsafe"
)

// The ioctl structs are handed straight to the kernel, so their layout must
// match the C definitions exactly. A mismatch does not fail loudly: the kernel
// reads a pointer from the wrong offset and the read returns garbage, which on
// this hardware looks exactly like the seating fault the bench already chased
// once.
//
// i2c_linux.go carries compile-time assertions for the same thing, which is
// what actually covers 32-bit ARM — nobody here has a Pi Zero to run a test
// on, but `GOARCH=arm go build` proves the layout anyway. This test states the
// expectations in a form a reader can follow, and checks the field offsets the
// constants cannot.
func TestIoctlStructLayout(t *testing.T) {
	ptr := unsafe.Sizeof(uintptr(0))

	t.Run("i2c_msg", func(t *testing.T) {
		// u16 addr, u16 flags, u16 len, then a pointer aligned to its own
		// width: 12 bytes on 32-bit, 16 on 64-bit.
		want := 6
		if pad := want % int(ptr); pad != 0 {
			want += int(ptr) - pad
		}
		want += int(ptr)

		if got := int(unsafe.Sizeof(i2cMsg{})); got != want {
			t.Errorf("sizeof(i2cMsg) = %d, want %d on a %d-bit platform", got, want, ptr*8)
		}
		for _, f := range []struct {
			name string
			off  uintptr
			want uintptr
		}{
			{"addr", unsafe.Offsetof(i2cMsg{}.addr), 0},
			{"flags", unsafe.Offsetof(i2cMsg{}.flags), 2},
			{"len", unsafe.Offsetof(i2cMsg{}.len), 4},
		} {
			if f.off != f.want {
				t.Errorf("i2cMsg.%s is at offset %d, want %d", f.name, f.off, f.want)
			}
		}
		// The pointer must be aligned to its own width, or the kernel reads
		// a shifted address.
		if off := unsafe.Offsetof(i2cMsg{}.buf); off%ptr != 0 {
			t.Errorf("i2cMsg.buf is at offset %d, not aligned to %d", off, ptr)
		}
	})

	t.Run("i2c_rdwr_ioctl_data", func(t *testing.T) {
		// A pointer followed by a u32, padded out to the pointer's alignment.
		want := int(ptr) + 4
		if pad := want % int(ptr); pad != 0 {
			want += int(ptr) - pad
		}
		if got := int(unsafe.Sizeof(i2cRdwrData{})); got != want {
			t.Errorf("sizeof(i2cRdwrData) = %d, want %d on a %d-bit platform", got, want, ptr*8)
		}
		if off := unsafe.Offsetof(i2cRdwrData{}.msgs); off != 0 {
			t.Errorf("i2cRdwrData.msgs is at offset %d, want 0", off)
		}
	})
}

// The constants are copied from kernel headers, so pin them against a typo.
func TestIoctlConstants(t *testing.T) {
	if iocRdwr != 0x0707 {
		t.Errorf("I2C_RDWR = %#04x, want 0x0707", iocRdwr)
	}
	if msgRead != 0x0001 {
		t.Errorf("I2C_M_RD = %#04x, want 0x0001", msgRead)
	}
}

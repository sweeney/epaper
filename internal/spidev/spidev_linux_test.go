//go:build linux

package spidev

import (
	"os"
	"path/filepath"
	"testing"
)

// The ioctl request numbers are built rather than pasted, so that the size
// fields are visibly correct. This checks the arithmetic against the values
// the kernel headers actually define — the one thing that cannot be verified
// by reading the code, and which fails silently if wrong: an ioctl with the
// wrong size field is rejected, or worse, reads the wrong number of bytes.
//
// From include/uapi/linux/spi/spidev.h:
//
//	#define SPI_IOC_WR_MODE          _IOW(SPI_IOC_MAGIC, 1, __u8)
//	#define SPI_IOC_WR_BITS_PER_WORD _IOW(SPI_IOC_MAGIC, 3, __u8)
//	#define SPI_IOC_WR_MAX_SPEED_HZ  _IOW(SPI_IOC_MAGIC, 4, __u32)
//
// with SPI_IOC_MAGIC = 'k'.
func TestIoctlRequestNumbers(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  uint32
		want uint32
	}{
		{"SPI_IOC_WR_MODE", iocWrMode, 0x40016B01},
		{"SPI_IOC_WR_BITS_PER_WORD", iocWrBitsPerWord, 0x40016B03},
		{"SPI_IOC_WR_MAX_SPEED_HZ", iocWrMaxSpeedHz, 0x40046B04},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %#08x, want %#08x", tc.name, tc.got, tc.want)
		}
	}
}

// iow's field packing, checked independently of the SPI constants above.
func TestIOW(t *testing.T) {
	for _, tc := range []struct {
		typ, nr, size uint32
		want          uint32
	}{
		{'k', 1, 1, 0x40016B01},
		{'k', 4, 4, 0x40046B04},
		{0, 0, 0, 0x40000000},       // direction bits alone
		{0xFF, 0xFF, 0, 0x4000FFFF}, // type and number at their limits
		{0, 0, 0x3FFF, 0x7FFF0000},  // size at its 14-bit limit
	} {
		if got := iow(tc.typ, tc.nr, tc.size); got != tc.want {
			t.Errorf("iow(%#x, %d, %d) = %#08x, want %#08x", tc.typ, tc.nr, tc.size, got, tc.want)
		}
	}
}

// bufsiz is a module parameter, so it is read rather than assumed — PLAN §9.4.
// Every way that read can go wrong has to fall back rather than produce a zero
// or negative chunk size, which would make Write loop forever or panic.
func TestReadBufsiz(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	for _, tc := range []struct {
		name string
		path string
		want int
	}{
		{"a real value", write("ok", "65536\n"), 65536},
		{"no trailing newline", write("nonl", "8192"), 8192},
		{"whitespace", write("ws", "  4096  \n"), 4096},
		{"the kernel default", write("def", "4096\n"), DefaultChunkSize},
		{"missing file", filepath.Join(dir, "nope"), DefaultChunkSize},
		{"not a number", write("junk", "banana\n"), DefaultChunkSize},
		{"empty", write("empty", ""), DefaultChunkSize},
		{"zero", write("zero", "0\n"), DefaultChunkSize},
		{"negative", write("neg", "-1\n"), DefaultChunkSize},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := readBufsizFrom(tc.path); got != tc.want {
				t.Errorf("readBufsizFrom(%s) = %d, want %d", tc.name, got, tc.want)
			}
		})
	}

	// Whatever happens, the result must be usable as a chunk size.
	for _, name := range []string{"nope", "junk", "zero", "neg"} {
		if got := readBufsizFrom(filepath.Join(dir, name)); got <= 0 {
			t.Errorf("readBufsizFrom(%s) = %d; a non-positive chunk size would hang Write", name, got)
		}
	}
}

// Opening a device that is not there must fail cleanly rather than panic, and
// say which path it tried.
func TestOpenMissingDevice(t *testing.T) {
	_, err := Open(Config{Path: filepath.Join(t.TempDir(), "spidev-nope"), Mode: Mode0, SpeedHz: 1_000_000})
	if err == nil {
		t.Fatal("Open() on a missing device = nil error")
	}
	if !contains(err.Error(), "spidev-nope") {
		t.Errorf("error %q does not name the device it tried", err)
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

//go:build linux

package spidev

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// recordingWriter keeps each Write separately, which is the only way to see
// how a buffer was split.
type recordingWriter struct {
	writes  [][]byte
	failAt  int // 1-based call to fail on; 0 means never
	calls   int
	failErr error
}

func (r *recordingWriter) Write(p []byte) (int, error) {
	r.calls++
	if r.failAt != 0 && r.calls == r.failAt {
		return 0, r.failErr
	}
	r.writes = append(r.writes, append([]byte(nil), p...))
	return len(p), nil
}

// The framebuffer is 30,000 bytes and the default bufsiz is 4096, so every
// refresh goes through this loop eight times. A write longer than bufsiz is
// rejected by the kernel; a short one silently truncates the image.
func TestWriteChunks(t *testing.T) {
	for _, tc := range []struct {
		name      string
		total     int
		chunk     int
		wantCalls int
	}{
		{"the real framebuffer", 30000, 4096, 8},
		{"exactly one chunk", 4096, 4096, 1},
		{"one byte over", 4097, 4096, 2},
		{"one byte under", 4095, 4096, 1},
		{"smaller than a chunk", 10, 4096, 1},
		{"a single byte", 1, 4096, 1},
		{"chunk of one", 5, 1, 5},
		{"a larger bufsiz", 30000, 65536, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Fill with a recognisable pattern so reassembly is meaningful.
			src := make([]byte, tc.total)
			for i := range src {
				src[i] = byte(i)
			}

			w := &recordingWriter{}
			if err := writeChunks(w, src, tc.chunk); err != nil {
				t.Fatalf("writeChunks(): %v", err)
			}

			if len(w.writes) != tc.wantCalls {
				t.Errorf("%d writes, want %d", len(w.writes), tc.wantCalls)
			}
			// No single write may exceed the chunk size, or the kernel
			// rejects it.
			for i, chunk := range w.writes {
				if len(chunk) > tc.chunk {
					t.Errorf("write %d is %d bytes, over the %d limit", i, len(chunk), tc.chunk)
				}
				if len(chunk) == 0 {
					t.Errorf("write %d is empty", i)
				}
			}
			// And the pieces must reassemble into exactly the original: a
			// dropped or duplicated byte shifts every pixel after it.
			var got []byte
			for _, chunk := range w.writes {
				got = append(got, chunk...)
			}
			if !bytes.Equal(got, src) {
				t.Errorf("reassembled %d bytes, want %d, equal=%v", len(got), len(src), bytes.Equal(got, src))
			}
		})
	}
}

func TestWriteChunksEmpty(t *testing.T) {
	w := &recordingWriter{}
	if err := writeChunks(w, nil, 4096); err != nil {
		t.Fatalf("writeChunks(nil): %v", err)
	}
	if len(w.writes) != 0 {
		t.Errorf("%d writes for an empty buffer, want 0", len(w.writes))
	}
}

// A failure part-way through must be reported, not swallowed — a partially
// written framebuffer is a corrupted image.
func TestWriteChunksReportsFailure(t *testing.T) {
	boom := errors.New("EIO")
	w := &recordingWriter{failAt: 3, failErr: boom}

	err := writeChunks(w, make([]byte, 30000), 4096)
	if !errors.Is(err, boom) {
		t.Fatalf("writeChunks() = %v, want the underlying error", err)
	}
	if !strings.Contains(err.Error(), "4096") {
		t.Errorf("error %q does not say how much it was writing", err)
	}
}

// A non-positive chunk size would loop forever. readBufsiz cannot produce one,
// but the guard is cheap and the failure mode is a hung refresh.
func TestWriteChunksRejectsABadChunkSize(t *testing.T) {
	for _, size := range []int{0, -1} {
		if err := writeChunks(&recordingWriter{}, []byte{1, 2, 3}, size); err == nil {
			t.Errorf("writeChunks(chunk=%d) = nil error; that would loop forever", size)
		}
	}
}

// Write on a closed device must report, not panic on a nil file.
func TestWriteAfterClose(t *testing.T) {
	d := &Device{}
	if err := d.Write([]byte{1}); err == nil {
		t.Error("Write() on a closed device = nil error")
	}
	if err := d.Close(); err != nil {
		t.Errorf("Close() on a never-opened device = %v", err)
	}
	if got := d.ChunkSize(); got != 0 {
		t.Errorf("ChunkSize() on a zero Device = %d", got)
	}
}

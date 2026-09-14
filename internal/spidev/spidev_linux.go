//go:build linux

package spidev

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// SPI ioctl request numbers, built with the asm-generic _IOC encoding rather
// than pasted as magic constants, so the size fields are visibly correct.
//
// From the kernel's include/uapi/linux/spi/spidev.h.
var (
	iocWrMode        = iow('k', 1, 1) // __u8
	iocWrBitsPerWord = iow('k', 3, 1) // __u8
	iocWrMaxSpeedHz  = iow('k', 4, 4) // __u32
)

// iow builds a _IOW ioctl request. The direction bits are the top two, then
// the argument size, then the type and number.
func iow(typ, nr, size uint32) uint32 {
	const (
		dirWrite  = 1
		nrBits    = 8
		typeBits  = 8
		sizeBits  = 14
		nrShift   = 0
		typeShift = nrShift + nrBits
		sizeShift = typeShift + typeBits
		dirShift  = sizeShift + sizeBits
	)
	return dirWrite<<dirShift | size<<sizeShift | typ<<typeShift | nr<<nrShift
}

// bufsizPath is where the kernel reports spidev's maximum transfer size.
const bufsizPath = "/sys/module/spidev/parameters/bufsiz"

// Device is an open SPI device.
type Device struct {
	mu        sync.Mutex
	f         *os.File
	chunkSize int
}

// Open configures and opens an SPI device.
//
// The transfer chunk size is read from the kernel rather than assumed: bufsiz
// is a module parameter, and a write longer than it fails outright. Reading it
// costs one small file open and removes a class of "works on my Pi" bug.
func Open(cfg Config) (*Device, error) {
	f, err := os.OpenFile(cfg.Path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("spidev: open %s: %w", cfg.Path, err)
	}

	d := &Device{f: f, chunkSize: readBufsiz()}

	mode := uint8(cfg.Mode)
	if err := ioctlPtr(f.Fd(), iocWrMode, unsafe.Pointer(&mode)); err != nil {
		f.Close()
		return nil, fmt.Errorf("spidev: %s: set mode %d: %w", cfg.Path, cfg.Mode, err)
	}
	bits := uint8(8)
	if err := ioctlPtr(f.Fd(), iocWrBitsPerWord, unsafe.Pointer(&bits)); err != nil {
		f.Close()
		return nil, fmt.Errorf("spidev: %s: set bits per word: %w", cfg.Path, err)
	}
	speed := cfg.SpeedHz
	if err := ioctlPtr(f.Fd(), iocWrMaxSpeedHz, unsafe.Pointer(&speed)); err != nil {
		f.Close()
		return nil, fmt.Errorf("spidev: %s: set speed %d Hz: %w", cfg.Path, cfg.SpeedHz, err)
	}
	return d, nil
}

// Write sends bytes, splitting them into transfers the kernel will accept.
//
// Chip select is NOT touched here. On this board CS is driven as a GPIO by the
// caller, which is what lets it stay asserted across all the chunks of a
// 30,000-byte framebuffer — the panel treats one long assertion as one
// transfer.
func (d *Device) Write(b []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.f == nil {
		return fmt.Errorf("spidev: write: device is closed")
	}
	for len(b) > 0 {
		n := min(len(b), d.chunkSize)
		if _, err := d.f.Write(b[:n]); err != nil {
			return fmt.Errorf("spidev: write %d bytes: %w", n, err)
		}
		b = b[n:]
	}
	return nil
}

// ChunkSize reports the largest single transfer this device will make.
func (d *Device) ChunkSize() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.chunkSize
}

// Close releases the device. It is safe to call more than once.
func (d *Device) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.f == nil {
		return nil
	}
	err := d.f.Close()
	d.f = nil
	if err != nil {
		return fmt.Errorf("spidev: close: %w", err)
	}
	return nil
}

// readBufsiz returns the kernel's configured maximum SPI transfer size,
// falling back to the compiled-in default if sysfs does not say.
func readBufsiz() int {
	b, err := os.ReadFile(bufsizPath)
	if err != nil {
		return DefaultChunkSize
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || n <= 0 {
		return DefaultChunkSize
	}
	return n
}

func ioctlPtr(fd uintptr, req uint32, arg unsafe.Pointer) error {
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, uintptr(req), uintptr(arg)); errno != 0 {
		return errno
	}
	return nil
}

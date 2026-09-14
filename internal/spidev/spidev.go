// Package spidev writes to an SPI device through /dev/spidev*.
//
// It is deliberately write-only. An e-ink controller is told things; it never
// answers, and the one signal it sends back is a GPIO line, not a MOSI
// response. Implementing only what is used keeps the ioctl surface small.
//
// This package is a thin wrapper over the kernel interface. Its real test is
// the hardware run — see PLAN §6.5 — so there is no coverage target here.
package spidev

import "errors"

// ErrUnsupported means this platform has no /dev/spidev. The package builds
// everywhere so that the mock path stays portable; it only works on Linux.
var ErrUnsupported = errors.New("spidev: only supported on Linux")

// DefaultChunkSize is the fallback transfer size when the kernel's real
// bufsiz cannot be read.
//
// 4096 is spidev's compiled-in default, but it is a module parameter and can
// differ, so [Open] reads the real value from sysfs and only falls back to
// this. Writing more than bufsiz in one call fails, and our framebuffer is
// 30,000 bytes.
const DefaultChunkSize = 4096

// Mode is the SPI clock polarity and phase. The JD79668 uses mode 0.
type Mode uint8

// SPI modes, by the usual (CPOL, CPHA) numbering.
const (
	Mode0 Mode = 0
	Mode1 Mode = 1
	Mode2 Mode = 2
	Mode3 Mode = 3
)

// Config describes how to open an SPI device.
type Config struct {
	// Path is the device node, e.g. "/dev/spidev0.0".
	Path string
	// Mode is the clock polarity and phase.
	Mode Mode
	// SpeedHz is the maximum clock rate.
	SpeedHz uint32
}

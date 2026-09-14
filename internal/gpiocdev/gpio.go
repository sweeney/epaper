// Package gpiocdev drives GPIO lines through the character device interface.
//
// It wraps github.com/warthog618/go-gpiocdev so that the third-party
// dependency has exactly one point of contact with the rest of the library. If
// that package ever needs replacing — it has a single maintainer, which PLAN
// §5.1 records as a risk — only this file changes.
//
// The old sysfs GPIO interface is deliberately not used: it is deprecated, and
// it cannot express the bias setting the BUSY line needs.
//
// This package is a thin wrapper over the kernel interface. Its real test is
// the hardware run — see PLAN §6.5 — so there is no coverage target here.
package gpiocdev

import "errors"

// Errors this package returns. Match with [errors.Is].
var (
	// ErrUnsupported means this platform has no GPIO character device. The
	// package builds everywhere so that the mock path stays portable.
	ErrUnsupported = errors.New("gpiocdev: only supported on Linux")

	// ErrLineBusy means another driver already owns the line. On a Pi this
	// is nearly always the kernel SPI driver holding chip-select, which is
	// fixed with dtoverlay=spi0-0cs; the board layer turns this into that
	// advice.
	ErrLineBusy = errors.New("gpiocdev: line is already claimed")

	// ErrNoChip means no GPIO chip could be found.
	ErrNoChip = errors.New("gpiocdev: no GPIO chip found")
)

// Config describes the lines to request.
type Config struct {
	// Chip is the chip name, e.g. "gpiochip0". Empty means auto-detect.
	Chip string

	// Consumer is the name the kernel reports as owning these lines. It
	// shows up in gpioinfo, so make it recognisable.
	Consumer string

	// Outputs maps a line offset to the value it should be driven to
	// immediately on request. Setting the initial value as part of the
	// request avoids a glitch between acquiring a line and driving it.
	Outputs map[int]int

	// Inputs lists line offsets to read. They are requested with the
	// internal pull-up enabled, which is what the panel's BUSY line needs.
	//
	// Note the consequence: a DISCONNECTED input reads high, which for BUSY
	// means "ready". That ambiguity is handled a layer up, in the driver.
	Inputs []int
}

// Package i2c reads from an I2C device through /dev/i2c-*.
//
// It exists for one job: reading the Inky HAT's identification EEPROM. That
// part needs a two-byte register address written before the read, in a single
// combined transaction with a repeated start.
//
// This matters more than it sounds. Byte-mode SMBus reads — what `i2cdump -b`
// does — return convincing garbage from this EEPROM: `01 ff ff…` on one run
// and `30`/`31` on the next. On the bench that sent us chasing a seating fault
// on perfectly good hardware. Always use the combined transaction below.
//
// This package is a thin wrapper over the kernel interface. Its real test is
// the hardware run — see PLAN §6.5 — so there is no coverage target here.
package i2c

import "errors"

// ErrUnsupported means this platform has no /dev/i2c. The package builds
// everywhere so that the mock path stays portable; it only works on Linux.
var ErrUnsupported = errors.New("i2c: only supported on Linux")

package inky

import (
	"context"
	"fmt"
	"time"

	"github.com/sweeney/epaper/internal/gpiocdev"
	"github.com/sweeney/epaper/internal/spidev"
)

// Signal levels. Every one of these lines is active low, which is easy to
// invert by accident, so they are named rather than written as 0 and 1.
const (
	assert  = 0 // active low: pull down to assert
	release = 1

	levelCommand = 0 // DC low  = the byte is a command
	levelData    = 1 // DC high = the byte is data

	busyBusy  = 0 // BUSY low  = the panel is working
	busyReady = 1 // BUSY high = the panel is idle
)

// Reset timing, from inky_jd79668.py Inky.setup(). Unlike the per-command
// delay in PLAN §9.3, these are genuine hardware requirements: the controller
// needs the reset line held and then needs time to come back up.
const (
	resetHold     = 30 * time.Millisecond
	resetSettle   = 30 * time.Millisecond
	busyPollEvery = 10 * time.Millisecond
)

// spiWriter and gpioLines are the slivers of the transports that conn
// actually uses. They exist so this file — which is where an active-low line
// gets inverted by accident, and where a mistake is a blank panel — can be
// tested against fakes rather than only on a Pi.
type spiWriter interface {
	Write(b []byte) error
	Close() error
}

type gpioLines interface {
	Set(offset, value int) error
	Get(offset int) (int, error)
	Close() error
}

// Compile-time proof the real transports satisfy them.
var (
	_ spiWriter = (*spidev.Device)(nil)
	_ gpioLines = (*gpiocdev.Lines)(nil)
)

// conn implements jd79668.Conn over an SPI device and four GPIO lines.
type conn struct {
	spi  spiWriter
	gpio gpioLines
	pins Pins
}

// Command sends a command byte and its payload in one chip-select assertion.
//
// Chip select stays asserted for the whole thing, payload included. That is
// what lets a 30,000-byte framebuffer go out as many SPI transfers but one
// logical transaction — the controller sees a single continuous assertion.
func (c *conn) Command(cmd byte, data []byte) error {
	if err := c.gpio.Set(c.pins.ChipSelect, assert); err != nil {
		return err
	}
	// Release chip select whatever happens, so one failure does not leave the
	// bus held and every later command failing for a different reason.
	defer func() { _ = c.gpio.Set(c.pins.ChipSelect, release) }()

	if err := c.gpio.Set(c.pins.DataCommand, levelCommand); err != nil {
		return err
	}
	if err := c.spi.Write([]byte{cmd}); err != nil {
		return fmt.Errorf("sending command 0x%02X: %w", cmd, err)
	}

	if len(data) > 0 {
		if err := c.gpio.Set(c.pins.DataCommand, levelData); err != nil {
			return err
		}
		if err := c.spi.Write(data); err != nil {
			return fmt.Errorf("sending %d bytes of data for command 0x%02X: %w", len(data), cmd, err)
		}
		if err := c.gpio.Set(c.pins.DataCommand, levelCommand); err != nil {
			return err
		}
	}
	return nil
}

// Reset pulses the hardware reset line: low, hold, high, settle.
func (c *conn) Reset(ctx context.Context) error {
	if err := c.gpio.Set(c.pins.Reset, assert); err != nil {
		return err
	}
	if err := sleep(ctx, resetHold); err != nil {
		return err
	}
	if err := c.gpio.Set(c.pins.Reset, release); err != nil {
		return err
	}
	return sleep(ctx, resetSettle)
}

// WaitReady blocks until BUSY reports the panel idle.
//
// On timeout it returns [ErrBusyTimeout] rather than carrying on. The vendor
// library instead sleeps the full timeout and proceeds as though all were
// well, which turns a wiring fault into a display that silently shows nothing
// — the worst possible outcome on a panel nobody is watching.
func (c *conn) WaitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		v, err := c.gpio.Get(c.pins.Busy)
		if err != nil {
			return fmt.Errorf("reading BUSY: %w", err)
		}
		if v == busyReady {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("panel still busy after %s: %w", timeout, ErrBusyTimeout)
		}
		if err := sleep(ctx, busyPollEvery); err != nil {
			return err
		}
	}
}

// Close releases the SPI device and the GPIO lines, closing both even if the
// first fails.
func (c *conn) Close() error {
	var firstErr error
	if c.spi != nil {
		if err := c.spi.Close(); err != nil {
			firstErr = err
		}
	}
	if c.gpio != nil {
		if err := c.gpio.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// sleep waits, but gives up if the context is cancelled first. A refresh takes
// 25 seconds, so every wait in this package has to be interruptible.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

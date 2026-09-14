//go:build !linux

package i2c

// Bus is a stub so that this package compiles on non-Linux platforms, keeping
// the mock path portable. Every method reports [ErrUnsupported].
type Bus struct{}

// Open always fails off Linux.
func Open(string) (*Bus, error) { return nil, ErrUnsupported }

// ReadReg16 always fails off Linux.
func (b *Bus) ReadReg16(uint16, uint16, int) ([]byte, error) { return nil, ErrUnsupported }

// Close does nothing off Linux.
func (b *Bus) Close() error { return nil }

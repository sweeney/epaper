//go:build !linux

package spidev

// Device is a stub so that this package compiles on non-Linux platforms,
// keeping the mock path portable. Every method reports [ErrUnsupported].
type Device struct{}

// Open always fails off Linux.
func Open(Config) (*Device, error) { return nil, ErrUnsupported }

// Write always fails off Linux.
func (d *Device) Write([]byte) error { return ErrUnsupported }

// ChunkSize reports the fallback size off Linux.
func (d *Device) ChunkSize() int { return DefaultChunkSize }

// Close does nothing off Linux.
func (d *Device) Close() error { return nil }

//go:build !linux

package gpiocdev

// Lines is a stub so that this package compiles on non-Linux platforms,
// keeping the mock path portable. Every method reports [ErrUnsupported].
type Lines struct{}

// Open always fails off Linux.
func Open(Config) (*Lines, error) { return nil, ErrUnsupported }

// Set always fails off Linux.
func (l *Lines) Set(int, int) error { return ErrUnsupported }

// Get always fails off Linux.
func (l *Lines) Get(int) (int, error) { return 0, ErrUnsupported }

// Close does nothing off Linux.
func (l *Lines) Close() error { return nil }

// FindChip always fails off Linux.
func FindChip() (string, error) { return "", ErrUnsupported }

//go:build linux

package gpiocdev

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	gpiod "github.com/warthog618/go-gpiocdev"
	"golang.org/x/sys/unix"
)

// Lines is a set of requested GPIO lines, addressed by offset.
type Lines struct {
	mu    sync.Mutex
	lines map[int]*gpiod.Line
	chip  string
}

// Open requests the configured lines.
//
// Every line is requested individually rather than as one group, so a failure
// names the offset that is actually unavailable. On this hardware that matters:
// the one line that routinely fails is chip-select, and "line 8 is busy" is a
// far better starting point than "the request failed".
func Open(cfg Config) (*Lines, error) {
	chip := cfg.Chip
	if chip == "" {
		var err error
		if chip, err = FindChip(); err != nil {
			return nil, err
		}
	}

	l := &Lines{lines: make(map[int]*gpiod.Line), chip: chip}
	opts := []gpiod.LineReqOption{}
	if cfg.Consumer != "" {
		opts = append(opts, gpiod.WithConsumer(cfg.Consumer))
	}

	for offset, initial := range cfg.Outputs {
		line, err := gpiod.RequestLine(chip, offset,
			append(append([]gpiod.LineReqOption{}, opts...), gpiod.AsOutput(initial))...)
		if err != nil {
			l.Close()
			return nil, lineError(chip, offset, "output", err)
		}
		l.lines[offset] = line
	}
	for _, offset := range cfg.Inputs {
		line, err := gpiod.RequestLine(chip, offset,
			append(append([]gpiod.LineReqOption{}, opts...), gpiod.AsInput, gpiod.WithPullUp)...)
		if err != nil {
			l.Close()
			return nil, lineError(chip, offset, "input", err)
		}
		l.lines[offset] = line
	}
	return l, nil
}

// Set drives an output line.
func (l *Lines) Set(offset, value int) error {
	l.mu.Lock()
	line, ok := l.lines[offset]
	l.mu.Unlock()
	if !ok {
		return fmt.Errorf("gpiocdev: line %d was not requested", offset)
	}
	if err := line.SetValue(value); err != nil {
		return fmt.Errorf("gpiocdev: set line %d to %d: %w", offset, value, err)
	}
	return nil
}

// Get reads an input line.
func (l *Lines) Get(offset int) (int, error) {
	l.mu.Lock()
	line, ok := l.lines[offset]
	l.mu.Unlock()
	if !ok {
		return 0, fmt.Errorf("gpiocdev: line %d was not requested", offset)
	}
	v, err := line.Value()
	if err != nil {
		return 0, fmt.Errorf("gpiocdev: read line %d: %w", offset, err)
	}
	return v, nil
}

// Close releases every line. It is safe to call more than once, and it closes
// all the lines it can even if one fails.
func (l *Lines) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	var firstErr error
	for offset, line := range l.lines {
		if err := line.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("gpiocdev: close line %d: %w", offset, err)
		}
		delete(l.lines, offset)
	}
	return firstErr
}

// FindChip returns the name of the SoC's GPIO chip.
//
// Pi models expose more than one chip, and the header pins are on the one
// whose label names the pin controller — pinctrl-bcm2835, -bcm2711 or -bcm2712
// depending on the model. Matching the label rather than assuming gpiochip0
// is what keeps this working across Pi generations.
func FindChip() (string, error) {
	names := gpiod.Chips()
	if len(names) == 0 {
		return "", fmt.Errorf("gpiocdev: no /dev/gpiochip* devices "+
			"(is the user in the gpio group?): %w", ErrNoChip)
	}
	for _, name := range names {
		c, err := gpiod.NewChip(name)
		if err != nil {
			continue
		}
		label := c.Label
		c.Close()
		if strings.HasPrefix(label, "pinctrl-") {
			return name, nil
		}
	}
	return "", fmt.Errorf("gpiocdev: none of %v is a pin controller "+
		"(looked for a label starting \"pinctrl-\"): %w", names, ErrNoChip)
}

// lineError adds the context that makes a GPIO failure actionable, and marks
// the busy case so the board layer can give its own advice.
func lineError(chip string, offset int, dir string, err error) error {
	if errors.Is(err, unix.EBUSY) {
		return fmt.Errorf("gpiocdev: %s line %d (%s) is claimed by another driver: %w",
			chip, offset, dir, ErrLineBusy)
	}
	return fmt.Errorf("gpiocdev: request %s line %d (%s): %w", chip, offset, dir, err)
}

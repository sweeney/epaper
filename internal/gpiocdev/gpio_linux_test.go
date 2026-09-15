//go:build linux

package gpiocdev

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// A busy line must be distinguishable from every other failure, because the
// board layer turns exactly that case into the dtoverlay=spi0-0cs advice —
// which is a twenty-minute debugging session for anyone who has not met it.
//
// The mapping is the whole point of this function, and getting it wrong
// degrades gracefully into a useless error rather than failing loudly, so it
// is worth pinning.
func TestLineError(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		wantBusy bool
	}{
		{"EBUSY", unix.EBUSY, true},
		{"wrapped EBUSY", fmt.Errorf("requesting line: %w", unix.EBUSY), true},
		{"EACCES", unix.EACCES, false},
		{"ENOENT", unix.ENOENT, false},
		{"something else", errors.New("no idea"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := lineError("gpiochip0", 8, "output", tc.err)

			if errors.Is(got, ErrLineBusy) != tc.wantBusy {
				t.Errorf("errors.Is(_, ErrLineBusy) = %v, want %v", !tc.wantBusy, tc.wantBusy)
			}
			// Whatever the cause, the message has to name the line. "The
			// request failed" is not something anyone can act on.
			for _, want := range []string{"gpiochip0", "8", "output"} {
				if !strings.Contains(got.Error(), want) {
					t.Errorf("error %q does not mention %q", got, want)
				}
			}
			if !tc.wantBusy && !errors.Is(got, tc.err) {
				t.Errorf("error %q does not wrap the underlying %v", got, tc.err)
			}
		})
	}
}

// Set and Get on a line that was never requested must say so, rather than
// panicking on a nil map entry.
func TestUnrequestedLines(t *testing.T) {
	l := &Lines{lines: nil}

	if err := l.Set(8, 1); err == nil {
		t.Error("Set() on an unrequested line = nil error")
	} else if !strings.Contains(err.Error(), "8") {
		t.Errorf("error %q does not name the line", err)
	}

	if _, err := l.Get(17); err == nil {
		t.Error("Get() on an unrequested line = nil error")
	}

	// Closing a set that holds nothing is fine and idempotent.
	if err := l.Close(); err != nil {
		t.Errorf("Close() on an empty set = %v", err)
	}
	if err := l.Close(); err != nil {
		t.Errorf("second Close() = %v", err)
	}
}

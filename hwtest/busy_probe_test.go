//go:build hardware

package hwtest

import (
	"testing"
	"time"

	"github.com/sweeney/epaper/internal/gpiocdev"
)

// TestBusyProbe characterises the BUSY line, because two runs disagreed about
// it and a driver cannot be built on an unrepeatable measurement.
//
// It is a probe, not an assertion: it logs what it sees and only fails if the
// line is unreadable.
func TestBusyProbe(t *testing.T) {
	read := func(t *testing.T, label string) {
		t.Helper()
		lines, err := gpiocdev.Open(gpiocdev.Config{
			Consumer: "epaper-probe",
			Outputs:  map[int]int{pinCS: 1, pinDC: 0, pinReset: 1},
			Inputs:   []int{pinBusy},
		})
		if err != nil {
			t.Fatalf("%s: claiming lines: %v", label, err)
		}
		defer lines.Close()

		var vals []int
		for range 10 {
			v, err := lines.Get(pinBusy)
			if err != nil {
				t.Fatalf("%s: reading BUSY: %v", label, err)
			}
			vals = append(vals, v)
			time.Sleep(5 * time.Millisecond)
		}
		t.Logf("%-28s BUSY = %v", label, vals)
	}

	read(t, "fresh claim")
	read(t, "second claim")

	// Now with a reset pulse in between, which is what the driver does first.
	lines, err := gpiocdev.Open(gpiocdev.Config{
		Consumer: "epaper-probe",
		Outputs:  map[int]int{pinCS: 1, pinDC: 0, pinReset: 1},
		Inputs:   []int{pinBusy},
	})
	if err != nil {
		t.Fatalf("claiming lines: %v", err)
	}
	before, _ := lines.Get(pinBusy)
	if err := lines.Set(pinReset, 0); err != nil {
		t.Fatalf("asserting reset: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	during, _ := lines.Get(pinBusy)
	if err := lines.Set(pinReset, 1); err != nil {
		t.Fatalf("releasing reset: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	after, _ := lines.Get(pinBusy)

	var settle []int
	for range 10 {
		v, _ := lines.Get(pinBusy)
		settle = append(settle, v)
		time.Sleep(20 * time.Millisecond)
	}
	lines.Close()

	t.Logf("around a reset pulse: before=%d duringReset=%d after=%d", before, during, after)
	t.Logf("settling over 200ms:  %v", settle)
}

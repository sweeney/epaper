package inky

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// fakeSPI and fakeGPIO record what conn does, so the level conventions can be
// asserted. Every line on this board is active low, which is exactly the sort
// of thing that gets inverted once and then stays inverted.
type event struct {
	kind   string // "set", "write"
	line   int
	value  int
	nbytes int
	data   []byte
}

func (e event) String() string {
	if e.kind == "set" {
		return fmt.Sprintf("set line %d = %d", e.line, e.value)
	}
	return fmt.Sprintf("write %d bytes %#v", e.nbytes, e.data)
}

type fakeBus struct {
	events []event
	closed int
	busy   []int // values Get returns in turn; the last repeats
	failOn int   // 1-based call index to fail; 0 means never
	calls  int
	err    error
}

func (f *fakeBus) next() error {
	f.calls++
	if f.failOn != 0 && f.calls == f.failOn {
		return f.err
	}
	return nil
}

func (f *fakeBus) Write(b []byte) error {
	if err := f.next(); err != nil {
		return err
	}
	f.events = append(f.events, event{kind: "write", nbytes: len(b), data: append([]byte(nil), b...)})
	return nil
}

func (f *fakeBus) Set(line, value int) error {
	if err := f.next(); err != nil {
		return err
	}
	f.events = append(f.events, event{kind: "set", line: line, value: value})
	return nil
}

func (f *fakeBus) Get(int) (int, error) {
	if len(f.busy) == 0 {
		return busyReady, nil
	}
	v := f.busy[0]
	if len(f.busy) > 1 {
		f.busy = f.busy[1:]
	}
	return v, nil
}

func (f *fakeBus) Close() error { f.closed++; return nil }

func newConn(f *fakeBus) *conn {
	return &conn{spi: f, gpio: f, pins: DefaultPins}
}

// The exact line discipline for a command, from the vendor's _send_command:
// assert CS, DC low, send the command byte, DC high, send the data, DC low,
// release CS.
func TestCommandLineDiscipline(t *testing.T) {
	f := &fakeBus{}
	c := newConn(f)

	if err := c.Command(0x61, []byte{0x01, 0x90, 0x01, 0x2C}); err != nil {
		t.Fatalf("Command(): %v", err)
	}

	want := []event{
		{kind: "set", line: DefaultPins.ChipSelect, value: assert},
		{kind: "set", line: DefaultPins.DataCommand, value: levelCommand},
		{kind: "write", nbytes: 1, data: []byte{0x61}},
		{kind: "set", line: DefaultPins.DataCommand, value: levelData},
		{kind: "write", nbytes: 4, data: []byte{0x01, 0x90, 0x01, 0x2C}},
		{kind: "set", line: DefaultPins.DataCommand, value: levelCommand},
		{kind: "set", line: DefaultPins.ChipSelect, value: release},
	}
	assertEvents(t, f.events, want)
}

// A command with no payload must not touch DC a second time or send an empty
// write.
func TestCommandWithoutData(t *testing.T) {
	f := &fakeBus{}
	if err := newConn(f).Command(0x04, nil); err != nil {
		t.Fatalf("Command(): %v", err)
	}
	assertEvents(t, f.events, []event{
		{kind: "set", line: DefaultPins.ChipSelect, value: assert},
		{kind: "set", line: DefaultPins.DataCommand, value: levelCommand},
		{kind: "write", nbytes: 1, data: []byte{0x04}},
		{kind: "set", line: DefaultPins.ChipSelect, value: release},
	})
}

// Chip select is active LOW. Asserting it means driving 0. Getting this
// backwards produces a panel that never responds and no error anywhere.
func TestChipSelectIsActiveLow(t *testing.T) {
	if assert != 0 {
		t.Errorf("assert = %d, want 0 — every line on this board is active low", assert)
	}
	if release != 1 {
		t.Errorf("release = %d, want 1", release)
	}
	if levelCommand != 0 {
		t.Errorf("levelCommand = %d, want 0 — DC low means the byte is a command", levelCommand)
	}
}

// If a write fails mid-command, chip select must still be released. Leaving
// the bus held would make every later command fail for a different reason,
// hiding the real one.
func TestCommandReleasesChipSelectOnFailure(t *testing.T) {
	boom := errors.New("spi exploded")
	// Call 3 is the command-byte write.
	f := &fakeBus{failOn: 3, err: boom}

	err := newConn(f).Command(0x10, []byte{0x01})
	if !errors.Is(err, boom) {
		t.Fatalf("Command() error = %v, want the transport error", err)
	}
	last := f.events[len(f.events)-1]
	if last.kind != "set" || last.line != DefaultPins.ChipSelect || last.value != release {
		t.Errorf("last event was %v, want chip select released", last)
	}
}

// Reset is a low pulse: assert, hold, release, settle.
func TestResetPulse(t *testing.T) {
	f := &fakeBus{}
	started := time.Now()
	if err := newConn(f).Reset(context.Background()); err != nil {
		t.Fatalf("Reset(): %v", err)
	}
	assertEvents(t, f.events, []event{
		{kind: "set", line: DefaultPins.Reset, value: assert},
		{kind: "set", line: DefaultPins.Reset, value: release},
	})
	if elapsed := time.Since(started); elapsed < resetHold+resetSettle {
		t.Errorf("reset took %s, want at least %s — the hold is a hardware requirement",
			elapsed, resetHold+resetSettle)
	}
}

func TestResetHonoursContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := newConn(&fakeBus{}).Reset(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Reset() error = %v, want context.Canceled", err)
	}
}

func TestWaitReadyReturnsWhenReady(t *testing.T) {
	f := &fakeBus{busy: []int{busyBusy, busyBusy, busyReady}}
	if err := newConn(f).WaitReady(context.Background(), time.Second); err != nil {
		t.Errorf("WaitReady() = %v, want nil", err)
	}
}

// A panel that never reports ready must produce an error, not a silent
// 40-second sleep followed by carrying on regardless — which is what the
// vendor library does, and which turns a wiring fault into a display that
// shows nothing at all.
func TestWaitReadyTimesOut(t *testing.T) {
	f := &fakeBus{busy: []int{busyBusy}}
	err := newConn(f).WaitReady(context.Background(), 30*time.Millisecond)
	if !errors.Is(err, ErrBusyTimeout) {
		t.Errorf("WaitReady() error = %v, want ErrBusyTimeout", err)
	}
}

func TestWaitReadyHonoursContext(t *testing.T) {
	f := &fakeBus{busy: []int{busyBusy}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := newConn(f).WaitReady(ctx, time.Minute)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("WaitReady() error = %v, want the context deadline", err)
	}
}

func TestConnClose(t *testing.T) {
	if err := newConn(&fakeBus{}).Close(); err != nil {
		t.Errorf("Close() = %v", err)
	}
}

func assertEvents(t *testing.T, got, want []event) {
	t.Helper()
	for i := range max(len(got), len(want)) {
		switch {
		case i >= len(got):
			t.Errorf("event %d: missing, want %v", i, want[i])
		case i >= len(want):
			t.Errorf("event %d: unexpected %v", i, got[i])
		case got[i].String() != want[i].String():
			t.Errorf("event %d:\n  got  %v\n  want %v", i, got[i], want[i])
		}
	}
	if len(got) != len(want) {
		t.Fatalf("%d events, want %d", len(got), len(want))
	}
}

// Chip select must be released on EVERY failure path, not just the one
// convenient to test. Leaving the bus held makes every later command fail for
// a different reason, hiding the one that actually happened.
func TestCommandAlwaysReleasesChipSelect(t *testing.T) {
	boom := errors.New("transport failure")

	// Walk the failure point through the whole call: assert CS, set DC, write
	// the command, set DC for data, write the data, set DC back.
	for failAt := 1; failAt <= 6; failAt++ {
		f := &fakeBus{failOn: failAt, err: boom}

		err := newConn(f).Command(0x10, []byte{0x01, 0x02})
		if failAt == 1 {
			// The very first call IS the chip-select assert, so there is
			// nothing to release; it must still report.
			if !errors.Is(err, boom) {
				t.Errorf("failAt=1: Command() = %v, want the transport error", err)
			}
			continue
		}
		if !errors.Is(err, boom) {
			t.Errorf("failAt=%d: Command() = %v, want the transport error", failAt, err)
			continue
		}
		if len(f.events) == 0 {
			t.Errorf("failAt=%d: nothing was recorded", failAt)
			continue
		}
		last := f.events[len(f.events)-1]
		if last.kind != "set" || last.line != DefaultPins.ChipSelect || last.value != release {
			t.Errorf("failAt=%d: last event was %v, want chip select released", failAt, last)
		}
	}
}

// A reset that cannot drive the line has to report rather than carry on into
// an init sequence the panel never saw.
func TestResetReportsFailure(t *testing.T) {
	boom := errors.New("line gone")
	for failAt := 1; failAt <= 2; failAt++ {
		f := &fakeBus{failOn: failAt, err: boom}
		if err := newConn(f).Reset(context.Background()); !errors.Is(err, boom) {
			t.Errorf("failAt=%d: Reset() = %v, want the transport error", failAt, err)
		}
	}
}

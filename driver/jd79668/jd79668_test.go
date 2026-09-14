package jd79668_test

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/driver/jd79668"
)

// op is one thing the driver did to the transport.
type op struct {
	kind string // "reset", "cmd", "wait"
	cmd  byte
	data []byte
}

func (o op) String() string {
	switch o.kind {
	case "cmd":
		if len(o.data) > 16 {
			return fmt.Sprintf("cmd 0x%02X [%d bytes]", o.cmd, len(o.data))
		}
		if len(o.data) == 0 {
			return fmt.Sprintf("cmd 0x%02X", o.cmd)
		}
		return fmt.Sprintf("cmd 0x%02X %#v", o.cmd, o.data)
	default:
		return o.kind
	}
}

// recordingConn records everything the driver sends, and can be told to fail
// at a chosen point so error paths get exercised too.
type recordingConn struct {
	ops []op

	failAt  int // 1-based index of the call to fail on; 0 means never
	calls   int
	failErr error
}

func (c *recordingConn) next() error {
	c.calls++
	if c.failAt != 0 && c.calls == c.failAt {
		return c.failErr
	}
	return nil
}

func (c *recordingConn) Command(cmd byte, data []byte) error {
	if err := c.next(); err != nil {
		return err
	}
	c.ops = append(c.ops, op{kind: "cmd", cmd: cmd, data: append([]byte(nil), data...)})
	return nil
}

func (c *recordingConn) Reset(context.Context) error {
	if err := c.next(); err != nil {
		return err
	}
	c.ops = append(c.ops, op{kind: "reset"})
	return nil
}

func (c *recordingConn) WaitReady(context.Context, time.Duration) error {
	if err := c.next(); err != nil {
		return err
	}
	c.ops = append(c.ops, op{kind: "wait"})
	return nil
}

// newDevice returns a driver with no command delay, so tests run in
// microseconds rather than five seconds.
func newDevice(t *testing.T, conn jd79668.Conn) *jd79668.Device {
	t.Helper()
	d, err := jd79668.New(conn, jd79668.Config{
		Width: 400, Height: 300,
		CommandDelay: -1,
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return d
}

// THE test. It pins every byte the driver emits, in order, against PLAN §2.5
// and reference/vendor-inky/inky_jd79668.py.
//
// This is the part where a silent mistake produces a blank panel and a very
// bad afternoon, 25 seconds at a time. Here it runs in microseconds.
func TestShowEmitsTheExactSequence(t *testing.T) {
	conn := &recordingConn{}
	d := newDevice(t, conn)

	img := d.NewImage()
	if err := d.Show(context.Background(), img); err != nil {
		t.Fatalf("Show(): %v", err)
	}

	frame := make([]byte, 30000) // a blank image packs to 30,000 zero bytes
	want := []op{
		{kind: "reset"},

		// --- init, from Inky.setup() ---
		{kind: "cmd", cmd: 0x4D, data: []byte{0x78}},
		{kind: "cmd", cmd: 0x00, data: []byte{0x0F, 0x29}},                               // PSR
		{kind: "cmd", cmd: 0x06, data: []byte{0x0D, 0x12, 0x24, 0x25, 0x12, 0x29, 0x10}}, // BTST_P
		{kind: "cmd", cmd: 0x30, data: []byte{0x08}},
		{kind: "cmd", cmd: 0x50, data: []byte{0x37}},                   // CDI
		{kind: "cmd", cmd: 0x61, data: []byte{0x01, 0x90, 0x01, 0x2C}}, // TRES = 400x300
		{kind: "cmd", cmd: 0xAE, data: []byte{0xCF}},
		{kind: "cmd", cmd: 0xB0, data: []byte{0x13}},
		{kind: "cmd", cmd: 0xBD, data: []byte{0x07}},
		{kind: "cmd", cmd: 0xBE, data: []byte{0xFE}},
		{kind: "cmd", cmd: 0xE9, data: []byte{0x01}},

		// --- refresh, from Inky._update() ---
		{kind: "cmd", cmd: 0x10, data: frame}, // DTM
		{kind: "cmd", cmd: 0x04},              // PON
		{kind: "wait"},
		{kind: "cmd", cmd: 0x12, data: []byte{0x00}}, // DRF
		{kind: "wait"},
		{kind: "cmd", cmd: 0x02, data: []byte{0x00}}, // POF
		{kind: "wait"},
		{kind: "cmd", cmd: 0x07, data: []byte{0xA5}}, // DSLP
	}

	assertOps(t, conn.ops, want)
}

// Init runs before EVERY refresh, because the previous one ended in deep
// sleep and the controller has forgotten its configuration.
func TestInitRunsBeforeEveryShow(t *testing.T) {
	conn := &recordingConn{}
	d := newDevice(t, conn)
	ctx := context.Background()

	for range 3 {
		if err := d.Show(ctx, d.NewImage()); err != nil {
			t.Fatalf("Show(): %v", err)
		}
	}

	resets, psr := 0, 0
	for _, o := range conn.ops {
		switch {
		case o.kind == "reset":
			resets++
		case o.kind == "cmd" && o.cmd == 0x00:
			psr++
		}
	}
	if resets != 3 {
		t.Errorf("%d resets over 3 shows, want 3", resets)
	}
	if psr != 3 {
		t.Errorf("%d PSR commands over 3 shows, want 3", psr)
	}
}

// The reset must be the very first thing, before anything waits on BUSY.
// With the panel asleep BUSY reads busy, so a wait beforehand would hang for
// the full timeout — measured on the bench, PLAN §2.3.
func TestResetComesFirstAndNothingWaitsBeforeIt(t *testing.T) {
	conn := &recordingConn{}
	d := newDevice(t, conn)
	if err := d.Show(context.Background(), d.NewImage()); err != nil {
		t.Fatalf("Show(): %v", err)
	}

	if len(conn.ops) == 0 || conn.ops[0].kind != "reset" {
		t.Fatalf("first op is %v, want reset", conn.ops[0])
	}
	for i, o := range conn.ops {
		if o.kind == "wait" {
			if !opsContainBefore(conn.ops[:i], "cmd", 0x04) {
				t.Errorf("a wait at index %d happens before PON; BUSY is not meaningful until then", i)
			}
			break
		}
	}
}

// The framebuffer goes out as DTM's payload, all 30,000 bytes of it.
func TestFramebufferIsSentWhole(t *testing.T) {
	conn := &recordingConn{}
	d := newDevice(t, conn)

	img := d.NewImage()
	for i := range img.Pix {
		img.Pix[i] = uint8(i % 4)
	}
	if err := d.Show(context.Background(), img); err != nil {
		t.Fatalf("Show(): %v", err)
	}

	want, err := epaper.Pack(img)
	if err != nil {
		t.Fatalf("Pack(): %v", err)
	}
	for _, o := range conn.ops {
		if o.kind == "cmd" && o.cmd == 0x10 {
			if len(o.data) != 30000 {
				t.Fatalf("DTM payload is %d bytes, want 30000", len(o.data))
			}
			if string(o.data) != string(want) {
				t.Fatal("DTM payload is not the packed image")
			}
			return
		}
	}
	t.Fatal("no DTM command was sent")
}

// TRES is computed from the configured geometry, not hardcoded. The vendor
// hardcodes 400x300; a different JD79668 board would need this right.
func TestResolutionCommandFollowsTheGeometry(t *testing.T) {
	conn := &recordingConn{}
	d, err := jd79668.New(conn, jd79668.Config{Width: 640, Height: 480, CommandDelay: -1})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	if err := d.Show(context.Background(), d.NewImage()); err != nil {
		t.Fatalf("Show(): %v", err)
	}
	for _, o := range conn.ops {
		if o.kind == "cmd" && o.cmd == 0x61 {
			want := []byte{0x02, 0x80, 0x01, 0xE0} // 640, 480 big-endian
			if string(o.data) != string(want) {
				t.Fatalf("TRES payload = %#v, want %#v", o.data, want)
			}
			return
		}
	}
	t.Fatal("no TRES command was sent")
}

// Nothing may reach the hardware for an image the driver cannot represent.
func TestShowValidatesBeforeTouchingTheHardware(t *testing.T) {
	for _, tc := range []struct {
		name string
		img  func(d *jd79668.Device) *image.Paletted
		want error
	}{
		{"nil", func(*jd79668.Device) *image.Paletted { return nil }, epaper.ErrBadImage},
		{
			"wrong size",
			func(*jd79668.Device) *image.Paletted {
				return image.NewPaletted(image.Rect(0, 0, 10, 10), jd79668.Palette.Colors())
			},
			epaper.ErrWrongSize,
		},
		{
			"wrong palette",
			func(*jd79668.Device) *image.Paletted {
				p := jd79668.Palette.Colors()
				p[2], p[3] = p[3], p[2]
				return image.NewPaletted(image.Rect(0, 0, 400, 300), p)
			},
			epaper.ErrPaletteMismatch,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := &recordingConn{}
			d := newDevice(t, conn)
			if err := d.Show(context.Background(), tc.img(d)); !errors.Is(err, tc.want) {
				t.Errorf("Show() error = %v, want %v", err, tc.want)
			}
			if len(conn.ops) != 0 {
				t.Errorf("%d operations reached the transport; a rejected image must touch nothing", len(conn.ops))
			}
		})
	}
}

// A transport failure part-way through must be reported, with enough context
// to say which step broke.
func TestShowReportsTransportFailures(t *testing.T) {
	for _, tc := range []struct {
		failAt int
		want   string
	}{
		{1, "reset"},
		{2, "init command 0x4D"},
		{13, "sending framebuffer"},
		{14, "power on"},
		{15, "waiting after power on"},
		{16, "refresh"},
		{20, "deep sleep"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			boom := errors.New("transport exploded")
			conn := &recordingConn{failAt: tc.failAt, failErr: boom}
			d := newDevice(t, conn)

			err := d.Show(context.Background(), d.NewImage())
			if err == nil {
				t.Fatalf("Show() = nil, want an error at call %d", tc.failAt)
			}
			if !errors.Is(err, boom) {
				t.Errorf("Show() error = %v, want it to wrap the transport error", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Show() error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestShowHonoursAnAlreadyCancelledContext(t *testing.T) {
	conn := &recordingConn{}
	d := newDevice(t, conn)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := d.Show(ctx, d.NewImage()); !errors.Is(err, context.Canceled) {
		t.Errorf("Show() error = %v, want context.Canceled", err)
	}
	if len(conn.ops) != 0 {
		t.Errorf("%d operations reached the transport after a cancelled context", len(conn.ops))
	}
}

func TestNewRejectsBadConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		conn jd79668.Conn
		cfg  jd79668.Config
	}{
		{"nil conn", nil, jd79668.Config{Width: 400, Height: 300}},
		{"no width", &recordingConn{}, jd79668.Config{Height: 300}},
		{"no height", &recordingConn{}, jd79668.Config{Width: 400}},
		{"negative", &recordingConn{}, jd79668.Config{Width: -1, Height: -1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := jd79668.New(tc.conn, tc.cfg); err == nil {
				t.Error("New() = nil error, want a failure")
			}
		})
	}
}

func TestDefaults(t *testing.T) {
	d, err := jd79668.New(&recordingConn{}, jd79668.Config{Width: 400, Height: 300})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	if got := d.Model(); got != "JD79668" {
		t.Errorf("Model() = %q, want the controller name", got)
	}
	if got := d.Bounds(); got != image.Rect(0, 0, 400, 300) {
		t.Errorf("Bounds() = %v, want 400x300", got)
	}
}

func TestModelOverride(t *testing.T) {
	d, err := jd79668.New(&recordingConn{}, jd79668.Config{
		Width: 400, Height: 300, Model: "Red/Yellow wHAT (JD79668)",
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	if got := d.Model(); got != "Red/Yellow wHAT (JD79668)" {
		t.Errorf("Model() = %q, want the EEPROM name", got)
	}
}

func TestClose(t *testing.T) {
	conn := &closableConn{}
	d := newDevice(t, conn)

	if err := d.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}
	if !conn.closed {
		t.Error("Close() did not close the transport")
	}
	if err := d.Close(); err != nil {
		t.Errorf("second Close() = %v, want nil — Close is idempotent", err)
	}
	if err := d.Show(context.Background(), d.NewImage()); !errors.Is(err, epaper.ErrClosed) {
		t.Errorf("Show() after Close = %v, want %v", err, epaper.ErrClosed)
	}
}

type closableConn struct {
	recordingConn
	closed bool
}

func (c *closableConn) Close() error { c.closed = true; return nil }

// PLAN §9.1: the palette order IS the wire format. Reordering it for tidiness
// would silently swap every panel's colours.
func TestPaletteOrderIsWireSignificant(t *testing.T) {
	want := []struct {
		ink epaper.Ink
		idx uint8
	}{
		{epaper.Black, 0},
		{epaper.White, 1},
		{epaper.Yellow, 2},
		{epaper.Red, 3},
	}
	if len(jd79668.Palette) != len(want) {
		t.Fatalf("palette has %d entries, want %d", len(jd79668.Palette), len(want))
	}
	for _, w := range want {
		got, ok := jd79668.Palette.Index(w.ink)
		if !ok {
			t.Errorf("palette has no %s", w.ink)
			continue
		}
		if got != w.idx {
			t.Errorf("%s is index %d, want %d — this is the value the controller receives", w.ink, got, w.idx)
		}
	}
	if err := jd79668.Palette.Validate(); err != nil {
		t.Errorf("Validate(): %v", err)
	}
	// A four-ink panel has no green, and must say so.
	if jd79668.Palette.Has(epaper.Green) {
		t.Error("palette claims to have green")
	}
}

func TestPaletteRGBIsOpaque(t *testing.T) {
	for _, e := range jd79668.Palette {
		if e.RGB.A != 255 {
			t.Errorf("%s has alpha %d, want 255", e.Ink, e.RGB.A)
		}
	}
	var _ color.Color = jd79668.Palette[0].RGB
}

// The driver must satisfy the interface consumers actually use.
var _ epaper.Device = (*jd79668.Device)(nil)

func assertOps(t *testing.T, got, want []op) {
	t.Helper()
	for i := range max(len(got), len(want)) {
		switch {
		case i >= len(got):
			t.Errorf("op %d: missing, want %v", i, want[i])
		case i >= len(want):
			t.Errorf("op %d: unexpected %v", i, got[i])
		case got[i].kind != want[i].kind ||
			got[i].cmd != want[i].cmd ||
			string(got[i].data) != string(want[i].data):
			t.Errorf("op %d:\n  got  %v\n  want %v", i, got[i], want[i])
		}
	}
	if len(got) != len(want) {
		t.Fatalf("%d operations, want %d", len(got), len(want))
	}
}

func opsContainBefore(ops []op, kind string, cmd byte) bool {
	for _, o := range ops {
		if o.kind == kind && o.cmd == cmd {
			return true
		}
	}
	return false
}

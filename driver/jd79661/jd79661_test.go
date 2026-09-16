package jd79661_test

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/driver/jd79661"
)

// The panel this controller ships on. Landscape, as the vendor presents it.
const (
	panelW = 250
	panelH = 122

	// The controller's own frame: the panel's short axis padded to a
	// multiple of eight, by the panel's long axis. 128 x 250, which packs to
	// 8,000 bytes rather than the 7,625 the visible pixels alone would need.
	ctrlW     = 128
	ctrlH     = 250
	frameSize = ctrlW * ctrlH / 4
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
func newDevice(t *testing.T, conn jd79661.Conn) *jd79661.Device {
	t.Helper()
	d, err := jd79661.New(conn, jd79661.Config{
		Width: panelW, Height: panelH,
		// A fake transport replies instantly, which is exactly what the
		// disconnected-BUSY check exists to catch. Disable it here and test
		// it deliberately in TestRefreshTooFastIsReported.
		MinRefreshTime: -1,
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return d
}

// THE test. It pins every byte the driver emits, in order, against
// reference/vendor-inky/inky_jd79661.py — Inky.setup() then Inky._update().
//
// This is the part where a silent mistake produces a blank panel and a very
// bad afternoon, 20 seconds at a time. Here it runs in microseconds.
func TestShowEmitsTheExactSequence(t *testing.T) {
	conn := &recordingConn{}
	d := newDevice(t, conn)

	if err := d.Show(context.Background(), d.NewImage()); err != nil {
		t.Fatalf("Show(): %v", err)
	}

	// A blank image is all index 0, and so is the padding, so the whole
	// frame packs to zero bytes.
	frame := make([]byte, frameSize)
	want := []op{
		{kind: "reset"},

		// --- init, from Inky.setup() ---
		{kind: "cmd", cmd: 0x4D, data: []byte{0x78}},
		{kind: "cmd", cmd: 0x00, data: []byte{0x0F, 0x29}},                               // PSR
		{kind: "cmd", cmd: 0x01, data: []byte{0x07, 0x00}},                               // PWR
		{kind: "cmd", cmd: 0x03, data: []byte{0x10, 0x54, 0x44}},                         // POFS
		{kind: "cmd", cmd: 0x06, data: []byte{0x0F, 0x0A, 0x2F, 0x25, 0x22, 0x2E, 0x21}}, // BTST_P
		{kind: "cmd", cmd: 0x50, data: []byte{0x37}},                                     // CDI
		{kind: "cmd", cmd: 0x60, data: []byte{0x02, 0x02}},                               // TCON
		{kind: "cmd", cmd: 0x61, data: []byte{0x00, 0x80, 0x00, 0xFA}},                   // TRES = 128x250
		{kind: "cmd", cmd: 0xE7, data: []byte{0x1C}},
		{kind: "cmd", cmd: 0xE3, data: []byte{0x22}}, // PWS
		{kind: "cmd", cmd: 0xB6, data: []byte{0x6F}},
		{kind: "cmd", cmd: 0xB4, data: []byte{0xD0}},
		{kind: "cmd", cmd: 0xE9, data: []byte{0x01}},
		{kind: "cmd", cmd: 0x30, data: []byte{0x08}},

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

// The init sequence is NOT the JD79668's. The two controllers share a command
// vocabulary and a refresh sequence, which makes it tempting to treat this as
// the same driver with different geometry; it is not. These four registers are
// written by one and not the other, in both directions.
//
// If someone ever "deduplicates" the two drivers, this is the test that should
// stop them.
func TestInitIsNotTheJD79668Sequence(t *testing.T) {
	conn := &recordingConn{}
	d := newDevice(t, conn)
	if err := d.Show(context.Background(), d.NewImage()); err != nil {
		t.Fatalf("Show(): %v", err)
	}

	sent := map[byte][]byte{}
	for _, o := range conn.ops {
		if o.kind == "cmd" {
			sent[o.cmd] = o.data
		}
	}

	// Ours, and not the JD79668's.
	for _, cmd := range []byte{0x01, 0x03, 0x60, 0xE7, 0xE3, 0xB6, 0xB4} {
		if _, ok := sent[cmd]; !ok {
			t.Errorf("command 0x%02X is in inky_jd79661.py setup() but was not sent", cmd)
		}
	}
	// The JD79668's, and not ours.
	for _, cmd := range []byte{0xAE, 0xB0, 0xBD, 0xBE} {
		if _, ok := sent[cmd]; ok {
			t.Errorf("command 0x%02X belongs to the JD79668 and must not be sent here", cmd)
		}
	}
	// Shared command code, different payload — the easiest one to copy wrong.
	wantBoost := []byte{0x0F, 0x0A, 0x2F, 0x25, 0x22, 0x2E, 0x21}
	if got := sent[0x06]; string(got) != string(wantBoost) {
		t.Errorf("BTST_P payload = %#v, want %#v (the JD79668's is different)", got, wantBoost)
	}
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
// BUSY reads busy for as long as reset is asserted, so a wait beforehand is
// meaningless — characterised on the bench, PLAN §2.3.
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

// --------------------------------------------------------------------------
// The frame layout. This is what makes the JD79661 a different driver rather
// than a differently-sized JD79668, and it is the one part that cannot be
// eyeballed from the vendor source.
// --------------------------------------------------------------------------

// The rule, worked out from Inky.show() in the vendor source and verified
// against numpy — see PLAN §13:
//
//	controller (cx, cy)  <-  image (x = cy, y = H-1-cx),  black when cx >= H
//
// Single pixels are the sharpest possible statement of it: a transposition, a
// flip, or an off-by-one in the padding each move exactly one bit, and each
// would otherwise produce a picture that looks broadly plausible.
func TestFrameLayoutPlacesPixelsWhereTheControllerExpects(t *testing.T) {
	for _, tc := range []struct {
		name string
		x, y int
	}{
		{"top-left", 0, 0},
		{"top-right", panelW - 1, 0},
		{"bottom-left", 0, panelH - 1},
		{"bottom-right", panelW - 1, panelH - 1},
		{"off-centre", 173, 47},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := &recordingConn{}
			d := newDevice(t, conn)

			img := d.NewImage()
			// White everywhere, one red pixel. White is 1 and red is 3, so
			// every two-bit group is 0b01 except one, which is 0b11.
			white, _ := jd79661.Palette.Index(epaper.White)
			red, _ := jd79661.Palette.Index(epaper.Red)
			for i := range img.Pix {
				img.Pix[i] = white
			}
			img.SetColorIndex(tc.x, tc.y, red)

			if err := d.Show(context.Background(), img); err != nil {
				t.Fatalf("Show(): %v", err)
			}
			got := dtmPayload(t, conn.ops)

			cx, cy := panelH-1-tc.y, tc.x
			n := cy*ctrlW + cx // pixel index in the stream
			for i := range ctrlW * ctrlH {
				want := byte(white)
				switch {
				case i == n:
					want = red
				case i%ctrlW >= panelH:
					want = 0 // padding, which is black
				}
				if v := pixelAt(got, i); v != want {
					t.Fatalf("stream pixel %d (controller %d,%d) = %d, want %d",
						i, i%ctrlW, i/ctrlW, v, want)
				}
			}
		})
	}
}

// The padding is six rows of black at one specific end of the fast axis, not
// wherever is convenient. Put it at the wrong end and the image shifts by six
// pixels; leave it out and every row after the first is skewed.
func TestFrameIsPaddedAtTheFarEndOfTheFastAxis(t *testing.T) {
	conn := &recordingConn{}
	d := newDevice(t, conn)

	img := d.NewImage()
	white, _ := jd79661.Palette.Index(epaper.White)
	for i := range img.Pix {
		img.Pix[i] = white
	}
	if err := d.Show(context.Background(), img); err != nil {
		t.Fatalf("Show(): %v", err)
	}
	got := dtmPayload(t, conn.ops)

	for i := range ctrlW * ctrlH {
		cx := i % ctrlW
		want := byte(white)
		if cx >= panelH {
			want = 0
		}
		if v := pixelAt(got, i); v != want {
			t.Fatalf("controller (%d,%d) = %d, want %d — the padding is in the wrong place",
				cx, i/ctrlW, v, want)
		}
	}
}

// The frame is a fixed size set by the controller's geometry, not by the
// number of visible pixels. 250x122 is 30,500 pixels, which would be 7,625
// bytes; the controller wants 8,000.
func TestFrameIsTheControllersSizeNotThePanels(t *testing.T) {
	conn := &recordingConn{}
	d := newDevice(t, conn)
	if err := d.Show(context.Background(), d.NewImage()); err != nil {
		t.Fatalf("Show(): %v", err)
	}
	if got := len(dtmPayload(t, conn.ops)); got != frameSize {
		t.Errorf("DTM payload is %d bytes, want %d", got, frameSize)
	}
}

// TRES is computed from the geometry, not hardcoded. The vendor hardcodes
// 0x00,0x80,0x00,0xFA; a driver that does the same would be silently wrong on
// any other panel carrying this controller.
func TestResolutionCommandFollowsTheGeometry(t *testing.T) {
	for _, tc := range []struct {
		w, h int
		want []byte
	}{
		{250, 122, []byte{0x00, 0x80, 0x00, 0xFA}}, // the real panel: 122 pads to 128
		{200, 96, []byte{0x00, 0x60, 0x00, 0xC8}},  // already a multiple of 8: no padding
		{300, 121, []byte{0x00, 0x80, 0x01, 0x2C}}, // 121 pads to 128 too
	} {
		t.Run(fmt.Sprintf("%dx%d", tc.w, tc.h), func(t *testing.T) {
			conn := &recordingConn{}
			d, err := jd79661.New(conn, jd79661.Config{Width: tc.w, Height: tc.h, MinRefreshTime: -1})
			if err != nil {
				t.Fatalf("New(): %v", err)
			}
			if err := d.Show(context.Background(), d.NewImage()); err != nil {
				t.Fatalf("Show(): %v", err)
			}
			for _, o := range conn.ops {
				if o.kind == "cmd" && o.cmd == 0x61 {
					if string(o.data) != string(tc.want) {
						t.Fatalf("TRES payload = %#v, want %#v", o.data, tc.want)
					}
					return
				}
			}
			t.Fatal("no TRES command was sent")
		})
	}
}

// Nothing may reach the hardware for an image the driver cannot represent.
func TestShowValidatesBeforeTouchingTheHardware(t *testing.T) {
	for _, tc := range []struct {
		name string
		img  *image.Paletted
		want error
	}{
		{"nil", nil, epaper.ErrBadImage},
		{
			"wrong size",
			image.NewPaletted(image.Rect(0, 0, 10, 10), jd79661.Palette.Colors()),
			epaper.ErrWrongSize,
		},
		{
			"transposed",
			image.NewPaletted(image.Rect(0, 0, panelH, panelW), jd79661.Palette.Colors()),
			epaper.ErrWrongSize,
		},
		{
			"wrong palette",
			func() *image.Paletted {
				p := jd79661.Palette.Colors()
				p[2], p[3] = p[3], p[2]
				return image.NewPaletted(image.Rect(0, 0, panelW, panelH), p)
			}(),
			epaper.ErrPaletteMismatch,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := &recordingConn{}
			d := newDevice(t, conn)
			if err := d.Show(context.Background(), tc.img); !errors.Is(err, tc.want) {
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
		{16, "sending framebuffer"},
		{17, "power on"},
		{18, "waiting after power on"},
		{19, "refresh"},
		{23, "deep sleep"},
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
		conn jd79661.Conn
		cfg  jd79661.Config
	}{
		{"nil conn", nil, jd79661.Config{Width: panelW, Height: panelH}},
		{"no width", &recordingConn{}, jd79661.Config{Height: panelH}},
		{"no height", &recordingConn{}, jd79661.Config{Width: panelW}},
		{"negative", &recordingConn{}, jd79661.Config{Width: -1, Height: -1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := jd79661.New(tc.conn, tc.cfg); err == nil {
				t.Error("New() = nil error, want a failure")
			}
		})
	}
}

func TestDefaults(t *testing.T) {
	d, err := jd79661.New(&recordingConn{}, jd79661.Config{Width: panelW, Height: panelH})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	if got := d.Model(); got != "JD79661" {
		t.Errorf("Model() = %q, want the controller name", got)
	}
	if got := d.Bounds(); got != image.Rect(0, 0, panelW, panelH) {
		t.Errorf("Bounds() = %v, want %dx%d", got, panelW, panelH)
	}
}

func TestModelOverride(t *testing.T) {
	d, err := jd79661.New(&recordingConn{}, jd79661.Config{
		Width: panelW, Height: panelH, Model: "Red/Yellow pHAT (JD79661)",
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	if got := d.Model(); got != "Red/Yellow pHAT (JD79661)" {
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

// The palette order IS the wire format. Reordering it for tidiness would
// silently swap every panel's colours. Same order as the JD79668 — checked
// against inky_jd79661.py, which declares its own copy.
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
	if len(jd79661.Palette) != len(want) {
		t.Fatalf("palette has %d entries, want %d", len(jd79661.Palette), len(want))
	}
	for _, w := range want {
		got, ok := jd79661.Palette.Index(w.ink)
		if !ok {
			t.Errorf("palette has no %s", w.ink)
			continue
		}
		if got != w.idx {
			t.Errorf("%s is index %d, want %d — this is the value the controller receives", w.ink, got, w.idx)
		}
	}
	if err := jd79661.Palette.Validate(); err != nil {
		t.Errorf("Validate(): %v", err)
	}
	if jd79661.Palette.Has(epaper.Green) {
		t.Error("palette claims to have green")
	}
}

func TestPaletteRGBIsOpaque(t *testing.T) {
	for _, e := range jd79661.Palette {
		if e.RGB.A != 255 {
			t.Errorf("%s has alpha %d, want 255", e.Ink, e.RGB.A)
		}
	}
	var _ color.Color = jd79661.Palette[0].RGB
}

// The driver must satisfy the interface consumers actually use.
var _ epaper.Device = (*jd79661.Device)(nil)

// A disconnected BUSY line reads "ready" because of the host pull-up, so every
// wait returns at once and Show would succeed in milliseconds having drawn
// nothing. The elapsed time is what gives it away.
func TestRefreshTooFastIsReported(t *testing.T) {
	conn := &recordingConn{}
	d, err := jd79661.New(conn, jd79661.Config{
		Width: panelW, Height: panelH,
		MinRefreshTime: time.Second,
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	err = d.Show(context.Background(), d.NewImage())
	if !errors.Is(err, jd79661.ErrBusyNotConnected) {
		t.Fatalf("Show() error = %v, want ErrBusyNotConnected", err)
	}
	for _, want := range []string{"BUSY", "nothing was drawn"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// A slow enough refresh must NOT trip the check.
func TestRefreshOfPlausibleLengthIsAccepted(t *testing.T) {
	conn := &slowConn{delay: 20 * time.Millisecond}
	d, err := jd79661.New(conn, jd79661.Config{
		Width: panelW, Height: panelH,
		MinRefreshTime: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	if err := d.Show(context.Background(), d.NewImage()); err != nil {
		t.Errorf("Show() = %v, want nil", err)
	}
}

// slowConn takes a believable amount of time to report ready.
type slowConn struct {
	recordingConn
	delay time.Duration
}

func (c *slowConn) WaitReady(ctx context.Context, d time.Duration) error {
	time.Sleep(c.delay)
	return c.recordingConn.WaitReady(ctx, d)
}

// The default must be no delay — the same deliberate departure from the
// reference as the JD79668 driver makes, for the same measured reason.
func TestDefaultIsNoCommandDelay(t *testing.T) {
	if jd79661.DefaultCommandDelay != 0 {
		t.Errorf("DefaultCommandDelay = %v, want 0 — see PLAN §9.3", jd79661.DefaultCommandDelay)
	}
	if jd79661.VendorCommandDelay != 300*time.Millisecond {
		t.Errorf("VendorCommandDelay = %v, want the reference's 300ms", jd79661.VendorCommandDelay)
	}

	conn := &recordingConn{}
	d, err := jd79661.New(conn, jd79661.Config{Width: panelW, Height: panelH, MinRefreshTime: -1})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	started := time.Now()
	if err := d.Show(context.Background(), d.NewImage()); err != nil {
		t.Fatalf("Show(): %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Errorf("a refresh against a fake transport took %s; the driver is sleeping", elapsed)
	}
}

// ...but asking for the vendor's timing must genuinely slow it down, or the
// escape hatch is decorative.
func TestVendorCommandDelayIsHonoured(t *testing.T) {
	conn := &recordingConn{}
	d, err := jd79661.New(conn, jd79661.Config{
		Width: 8, Height: 8,
		CommandDelay:   20 * time.Millisecond,
		MinRefreshTime: -1,
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	started := time.Now()
	if err := d.Show(context.Background(), d.NewImage()); err != nil {
		t.Fatalf("Show(): %v", err)
	}
	// 19 commands at 20ms is 380ms; allow plenty of slack, the point is that
	// it is clearly not zero.
	if elapsed := time.Since(started); elapsed < 200*time.Millisecond {
		t.Errorf("a refresh with a 20ms command delay took %s; the delay is being ignored", elapsed)
	}
}

// Show is documented as safe for concurrent use: a service with a ticker and a
// webhook both drawing would otherwise interleave two framebuffers into one
// picture, 20 seconds later, intermittently — PLAN §9.6.
func TestShowSerialisesConcurrentCallers(t *testing.T) {
	conn := &recordingConn{}
	d := newDevice(t, conn)

	const callers = 8
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := d.Show(context.Background(), d.NewImage()); err != nil {
				t.Errorf("Show(): %v", err)
			}
		}()
	}
	wg.Wait()

	const opsPerRefresh = 23
	if got, want := len(conn.ops), callers*opsPerRefresh; got != want {
		t.Fatalf("%d operations from %d refreshes, want %d", got, callers, want)
	}
	for i := 0; i < len(conn.ops); i += opsPerRefresh {
		refresh := conn.ops[i : i+opsPerRefresh]
		if refresh[0].kind != "reset" {
			t.Fatalf("refresh starting at op %d begins with %v, want reset — the refreshes interleaved", i, refresh[0])
		}
		last := refresh[opsPerRefresh-1]
		if last.kind != "cmd" || last.cmd != cmdDSLP {
			t.Fatalf("refresh starting at op %d ends with %v, want deep sleep", i, last)
		}
		for _, o := range refresh[1 : opsPerRefresh-1] {
			if o.kind == "reset" {
				t.Fatalf("a second reset appeared inside the refresh starting at op %d", i)
			}
		}
	}
}

// cmdDSLP is unexported, so mirror it here rather than reaching in. If the
// driver ever changed it, the sequence test would fail first and loudly.
const cmdDSLP = 0x07

// --------------------------------------------------------------------------
// helpers
// --------------------------------------------------------------------------

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

// dtmPayload returns the framebuffer the driver sent.
func dtmPayload(t *testing.T, ops []op) []byte {
	t.Helper()
	for _, o := range ops {
		if o.kind == "cmd" && o.cmd == 0x10 {
			return o.data
		}
	}
	t.Fatal("no DTM command was sent")
	return nil
}

// pixelAt unpacks one two-bit pixel from a packed frame, most significant
// pixel first.
func pixelAt(frame []byte, n int) byte {
	return (frame[n/4] >> ((3 - n%4) * 2)) & 0x03
}

package e640_test

import (
	"context"
	"errors"
	"fmt"
	"image"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/driver/e640"
)

// The panel this controller ships on. Landscape, as the vendor presents it.
const (
	panelW = 600
	panelH = 400

	// The controller's own frame: portrait, the panel turned a quarter turn.
	// No padding this time — 400 is already even, which is all four-bit
	// packing needs. 240,000 pixels at two to a byte is 120,000 bytes.
	ctrlW     = 400
	ctrlH     = 600
	frameSize = ctrlW * ctrlH / 2
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

// newDevice returns a driver with no command delay and the too-fast check
// off, so tests run in microseconds against a transport that replies at once.
func newDevice(t *testing.T, conn e640.Conn) *e640.Device {
	t.Helper()
	d, err := e640.New(conn, e640.Config{Width: panelW, Height: panelH, MinRefreshTime: -1})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return d
}

// THE test. It pins every byte the driver emits, in order, against
// reference/vendor-inky/inky_e640.py — Inky.setup() then Inky._update().
func TestShowEmitsTheExactSequence(t *testing.T) {
	conn := &recordingConn{}
	d := newDevice(t, conn)

	if err := d.Show(context.Background(), d.NewImage()); err != nil {
		t.Fatalf("Show(): %v", err)
	}

	// A blank image is all palette index 0, black, which is wire value 0 —
	// so the whole frame packs to zero bytes.
	frame := make([]byte, frameSize)
	want := []op{
		{kind: "reset"},
		// setup() waits on BUSY straight after the reset pulse; the JD
		// controllers' vendor drivers do not.
		{kind: "wait"},

		// --- init, from Inky.setup() ---
		{kind: "cmd", cmd: 0xAA, data: []byte{0x49, 0x55, 0x20, 0x08, 0x09, 0x18}},
		{kind: "cmd", cmd: 0x01, data: []byte{0x3F}},                   // PWR
		{kind: "cmd", cmd: 0x00, data: []byte{0x5F, 0x69}},             // PSR
		{kind: "cmd", cmd: 0x05, data: []byte{0x40, 0x1F, 0x1F, 0x2C}}, // BTST1
		{kind: "cmd", cmd: 0x08, data: []byte{0x6F, 0x1F, 0x1F, 0x22}}, // BTST3
		{kind: "cmd", cmd: 0x06, data: []byte{0x6F, 0x1F, 0x17, 0x17}}, // BTST2
		{kind: "cmd", cmd: 0x03, data: []byte{0x00, 0x54, 0x00, 0x44}}, // POFS
		{kind: "cmd", cmd: 0x60, data: []byte{0x02, 0x00}},             // TCON
		{kind: "cmd", cmd: 0x30, data: []byte{0x08}},                   // PLL
		{kind: "cmd", cmd: 0x50, data: []byte{0x3F}},                   // CDI
		{kind: "cmd", cmd: 0x61, data: []byte{0x01, 0x90, 0x02, 0x58}}, // TRES = 400x600
		{kind: "cmd", cmd: 0xE3, data: []byte{0x2F}},                   // PWS
		{kind: "cmd", cmd: 0x82, data: []byte{0x01}},                   // VDCS

		// --- refresh, from Inky._update() ---
		{kind: "cmd", cmd: 0x10, data: frame}, // DTM1
		{kind: "cmd", cmd: 0x04},              // PON
		{kind: "wait"},
		{kind: "cmd", cmd: 0x06, data: []byte{0x6F, 0x1F, 0x17, 0x47}}, // BTST2, second setting
		{kind: "cmd", cmd: 0x12, data: []byte{0x00}},                   // DRF
		{kind: "wait"},
		{kind: "cmd", cmd: 0x02, data: []byte{0x00}}, // POF
		{kind: "wait"},
	}

	assertOps(t, conn.ops, want)
}

// BTST2 is written twice per refresh, with different last bytes: 0x17 during
// init and 0x47 once the panel is powered. They look like a copy-paste slip,
// and "fixing" either one to match the other is exactly the tidy-up that
// produces a blank panel. This test is here to make that a deliberate act.
func TestBoosterIsReprogrammedAfterPowerOn(t *testing.T) {
	conn := &recordingConn{}
	d := newDevice(t, conn)
	if err := d.Show(context.Background(), d.NewImage()); err != nil {
		t.Fatalf("Show(): %v", err)
	}

	var btst2 [][]byte
	sawPON := false
	for _, o := range conn.ops {
		if o.kind != "cmd" {
			continue
		}
		switch o.cmd {
		case 0x04:
			sawPON = true
		case 0x06:
			if len(btst2) == 1 && !sawPON {
				t.Error("the second BTST2 was sent before power on")
			}
			btst2 = append(btst2, o.data)
		}
	}
	if len(btst2) != 2 {
		t.Fatalf("BTST2 sent %d times, want 2", len(btst2))
	}
	if btst2[0][3] != 0x17 || btst2[1][3] != 0x47 {
		t.Errorf("BTST2 last bytes = 0x%02X then 0x%02X, want 0x17 then 0x47", btst2[0][3], btst2[1][3])
	}
}

// The vendor ends with power off and nothing else. The JD drivers send deep
// sleep; this one must not invent it, because nothing in inky_e640.py
// suggests the controller expects it.
func TestNoDeepSleepIsSent(t *testing.T) {
	conn := &recordingConn{}
	d := newDevice(t, conn)
	if err := d.Show(context.Background(), d.NewImage()); err != nil {
		t.Fatalf("Show(): %v", err)
	}
	for _, o := range conn.ops {
		if o.kind == "cmd" && o.cmd == 0x07 {
			t.Fatal("DSLP was sent; inky_e640.py never sends it")
		}
	}
}

// Init runs before EVERY refresh. The vendor calls setup() — including the
// reset — at the top of every _update().
func TestInitRunsBeforeEveryShow(t *testing.T) {
	conn := &recordingConn{}
	d := newDevice(t, conn)
	ctx := context.Background()

	for range 3 {
		if err := d.Show(ctx, d.NewImage()); err != nil {
			t.Fatalf("Show(): %v", err)
		}
	}

	resets, cmdh := 0, 0
	for _, o := range conn.ops {
		switch {
		case o.kind == "reset":
			resets++
		case o.kind == "cmd" && o.cmd == 0xAA:
			cmdh++
		}
	}
	if resets != 3 || cmdh != 3 {
		t.Errorf("%d resets and %d 0xAA commands over 3 shows, want 3 of each", resets, cmdh)
	}
}

// --------------------------------------------------------------------------
// The palette, and the gap in the wire values.
// --------------------------------------------------------------------------

// The palette is in the vendor's order — the order Inky.BLACK..Inky.GREEN
// number them, and the order set_image quantises into.
func TestPaletteOrder(t *testing.T) {
	want := []epaper.Ink{epaper.Black, epaper.White, epaper.Yellow, epaper.Red, epaper.Blue, epaper.Green}
	if len(e640.Palette) != len(want) {
		t.Fatalf("palette has %d entries, want %d", len(e640.Palette), len(want))
	}
	for i, ink := range want {
		if got := e640.Palette[i].Ink; got != ink {
			t.Errorf("palette[%d] = %s, want %s", i, got, ink)
		}
	}
	if err := e640.Palette.Validate(); err != nil {
		t.Errorf("Validate(): %v", err)
	}
	for _, e := range e640.Palette {
		if e.RGB.A != 255 {
			t.Errorf("%s has alpha %d, want 255", e.Ink, e.RGB.A)
		}
	}
	if e640.Palette.Has(epaper.Orange) {
		t.Error("palette claims to have orange; Spectra 6 has no orange ink")
	}
}

// The controller's colour codes skip 4: blue is sent as 5 and green as 6.
// This is the one place in the library where palette position is NOT the wire
// value, so it is pinned per ink, by what arrives in the frame.
//
// From inky_e640.py: BLACK = 0, WHITE = 1, YELLOW = 2, RED = 3, BLUE = 5,
// GREEN = 6, and set_image()'s remap = [0, 1, 2, 3, 5, 6].
func TestWireValuesSkipFour(t *testing.T) {
	for _, tc := range []struct {
		ink  epaper.Ink
		wire byte
	}{
		{epaper.Black, 0},
		{epaper.White, 1},
		{epaper.Yellow, 2},
		{epaper.Red, 3},
		{epaper.Blue, 5},
		{epaper.Green, 6},
	} {
		t.Run(tc.ink.String(), func(t *testing.T) {
			idx, ok := e640.Palette.Index(tc.ink)
			if !ok {
				t.Fatalf("palette has no %s", tc.ink)
			}
			conn := &recordingConn{}
			d := newDevice(t, conn)
			img := d.NewImage()
			for i := range img.Pix {
				img.Pix[i] = idx
			}
			if err := d.Show(context.Background(), img); err != nil {
				t.Fatalf("Show(): %v", err)
			}
			frame := dtmPayload(t, conn.ops)
			want := tc.wire<<4 | tc.wire
			for i, b := range frame {
				if b != want {
					t.Fatalf("frame byte %d = 0x%02X, want 0x%02X for a panel of solid %s", i, b, want, tc.ink)
				}
			}
		})
	}
}

// --------------------------------------------------------------------------
// The frame layout.
// --------------------------------------------------------------------------

// The rule, worked out from Inky.show() — numpy.rot90(buf, -1), flattened —
// and the same as the JD79661's without the padding:
//
//	controller (cx, cy)  <-  image (x = cy, y = H-1-cx)
//
// One pixel of red on white moves exactly one nibble; a transposition, a flip
// or a nibble-order slip each move it somewhere else.
func TestFrameLayoutPlacesPixelsWhereTheControllerExpects(t *testing.T) {
	white, _ := e640.Palette.Index(epaper.White)
	red, _ := e640.Palette.Index(epaper.Red)
	const wireWhite, wireRed = 1, 3

	for _, tc := range []struct {
		name string
		x, y int
	}{
		{"top-left", 0, 0},
		{"top-right", panelW - 1, 0},
		{"bottom-left", 0, panelH - 1},
		{"bottom-right", panelW - 1, panelH - 1},
		{"off-centre, odd stream index", 173, 46},
		{"off-centre, even stream index", 173, 47},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := &recordingConn{}
			d := newDevice(t, conn)

			img := d.NewImage()
			for i := range img.Pix {
				img.Pix[i] = white
			}
			img.SetColorIndex(tc.x, tc.y, red)

			if err := d.Show(context.Background(), img); err != nil {
				t.Fatalf("Show(): %v", err)
			}
			got := dtmPayload(t, conn.ops)

			cx, cy := panelH-1-tc.y, tc.x
			n := cy*ctrlW + cx
			for i := range ctrlW * ctrlH {
				want := byte(wireWhite)
				if i == n {
					want = wireRed
				}
				if v := nibbleAt(got, i); v != want {
					t.Fatalf("stream pixel %d (controller %d,%d) = %d, want %d",
						i, i%ctrlW, i/ctrlW, v, want)
				}
			}
		})
	}
}

func TestFrameIsTheControllersSize(t *testing.T) {
	conn := &recordingConn{}
	d := newDevice(t, conn)
	if err := d.Show(context.Background(), d.NewImage()); err != nil {
		t.Fatalf("Show(): %v", err)
	}
	if got := len(dtmPayload(t, conn.ops)); got != frameSize {
		t.Errorf("DTM payload is %d bytes, want %d", got, frameSize)
	}
}

// TRES is computed from the geometry, not hardcoded: the controller is told
// its own portrait frame, short axis first.
func TestResolutionCommandFollowsTheGeometry(t *testing.T) {
	for _, tc := range []struct {
		w, h int
		want []byte
	}{
		{600, 400, []byte{0x01, 0x90, 0x02, 0x58}}, // the real panel: 400 then 600
		{800, 480, []byte{0x01, 0xE0, 0x03, 0x20}}, // what a 7.3" frame would say
		{10, 4, []byte{0x00, 0x04, 0x00, 0x0A}},
	} {
		t.Run(fmt.Sprintf("%dx%d", tc.w, tc.h), func(t *testing.T) {
			conn := &recordingConn{}
			d, err := e640.New(conn, e640.Config{Width: tc.w, Height: tc.h, MinRefreshTime: -1})
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

// --------------------------------------------------------------------------
// Validation, errors and lifecycle — the same contract as every driver.
// --------------------------------------------------------------------------

func TestShowValidatesBeforeTouchingTheHardware(t *testing.T) {
	for _, tc := range []struct {
		name string
		img  *image.Paletted
		want error
	}{
		{"nil", nil, epaper.ErrBadImage},
		{
			"wrong size",
			image.NewPaletted(image.Rect(0, 0, 10, 10), e640.Palette.Colors()),
			epaper.ErrWrongSize,
		},
		{
			// The EEPROM describes this panel as 400x600. The picture is
			// not; a caller who believes the EEPROM must be told.
			"transposed",
			image.NewPaletted(image.Rect(0, 0, panelH, panelW), e640.Palette.Colors()),
			epaper.ErrWrongSize,
		},
		{
			"wrong palette",
			func() *image.Paletted {
				p := e640.Palette.Colors()
				p[4], p[5] = p[5], p[4]
				return image.NewPaletted(image.Rect(0, 0, panelW, panelH), p)
			}(),
			epaper.ErrPaletteMismatch,
		},
		{
			"index past the palette",
			func() *image.Paletted {
				img := image.NewPaletted(image.Rect(0, 0, panelW, panelH), e640.Palette.Colors())
				img.Pix[1234] = 6
				return img
			}(),
			epaper.ErrBadImage,
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

func TestShowReportsTransportFailures(t *testing.T) {
	for _, tc := range []struct {
		failAt int
		want   string
	}{
		{1, "reset"},
		{2, "waiting after reset"},
		{3, "init command 0xAA"},
		{16, "sending framebuffer"},
		{17, "power on"},
		{18, "waiting after power on"},
		{19, "booster"},
		{20, "refresh"},
		{21, "waiting after refresh"},
		{22, "power off"},
		{23, "waiting after power off"},
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
		conn e640.Conn
		cfg  e640.Config
	}{
		{"nil conn", nil, e640.Config{Width: panelW, Height: panelH}},
		{"no width", &recordingConn{}, e640.Config{Height: panelH}},
		{"no height", &recordingConn{}, e640.Config{Width: panelW}},
		{"negative", &recordingConn{}, e640.Config{Width: -1, Height: -1}},
		// The height is the controller's fast axis. Odd, and two pixels
		// sharing a byte would straddle two controller rows.
		{"odd height", &recordingConn{}, e640.Config{Width: panelW, Height: 399}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := e640.New(tc.conn, tc.cfg); err == nil {
				t.Error("New() = nil error, want a failure")
			}
		})
	}
}

func TestDefaults(t *testing.T) {
	d, err := e640.New(&recordingConn{}, e640.Config{Width: panelW, Height: panelH})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	if got := d.Model(); got != "E640" {
		t.Errorf("Model() = %q, want the controller name", got)
	}
	if got := d.Bounds(); got != image.Rect(0, 0, panelW, panelH) {
		t.Errorf("Bounds() = %v, want %dx%d", got, panelW, panelH)
	}
	if got := len(d.Palette()); got != 6 {
		t.Errorf("Palette() has %d inks, want 6", got)
	}
	if e640.DefaultCommandDelay != 0 {
		t.Errorf("DefaultCommandDelay = %v, want 0 — see PLAN §9.3", e640.DefaultCommandDelay)
	}
	if e640.VendorCommandDelay != 300*time.Millisecond {
		t.Errorf("VendorCommandDelay = %v, want the reference's 300ms", e640.VendorCommandDelay)
	}
}

func TestModelOverride(t *testing.T) {
	d, err := e640.New(&recordingConn{}, e640.Config{
		Width: panelW, Height: panelH, Model: "Spectra 6 4.0 600 x 400 (E640)",
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	if got := d.Model(); got != "Spectra 6 4.0 600 x 400 (E640)" {
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

var _ epaper.Device = (*e640.Device)(nil)

// A disconnected BUSY line reads "ready" because of the host pull-up. Only the
// elapsed time gives it away.
func TestRefreshTooFastIsReported(t *testing.T) {
	d, err := e640.New(&recordingConn{}, e640.Config{
		Width: panelW, Height: panelH,
		MinRefreshTime: time.Second,
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	err = d.Show(context.Background(), d.NewImage())
	if !errors.Is(err, e640.ErrBusyNotConnected) {
		t.Fatalf("Show() error = %v, want ErrBusyNotConnected", err)
	}
	for _, want := range []string{"BUSY", "nothing was drawn"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestRefreshOfPlausibleLengthIsAccepted(t *testing.T) {
	d, err := e640.New(&slowConn{delay: 20 * time.Millisecond}, e640.Config{
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

type slowConn struct {
	recordingConn
	delay time.Duration
}

func (c *slowConn) WaitReady(ctx context.Context, d time.Duration) error {
	time.Sleep(c.delay)
	return c.recordingConn.WaitReady(ctx, d)
}

func TestVendorCommandDelayIsHonoured(t *testing.T) {
	d, err := e640.New(&recordingConn{}, e640.Config{
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
	// 18 commands at 20ms is 360ms; the point is that it is clearly not zero.
	if elapsed := time.Since(started); elapsed < 200*time.Millisecond {
		t.Errorf("a refresh with a 20ms command delay took %s; the delay is being ignored", elapsed)
	}
}

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
		for _, o := range refresh[1:] {
			if o.kind == "reset" {
				t.Fatalf("a second reset appeared inside the refresh starting at op %d", i)
			}
		}
	}
}

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

// nibbleAt unpacks one four-bit pixel from a packed frame, high nibble first.
func nibbleAt(frame []byte, n int) byte {
	if n%2 == 0 {
		return frame[n/2] >> 4
	}
	return frame[n/2] & 0x0F
}

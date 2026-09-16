package jd79661_test

import (
	"context"
	"image"
	"os"
	"testing"

	"github.com/sweeney/epaper/driver/jd79661"
)

// The oracle test.
//
// testdata/frame/jd79661-frame.{idx,bin} is a matched pair captured on the Pi
// from the REAL vendor library, with the real pHAT attached: 30,500 palette
// indices in, the 8,000 bytes Pimoroni's driver would have sent to the panel
// out. Regenerate with tools/frame_jd79661.py — see testdata/README.md §4.
//
// Everything else in this package's tests asserts that the driver does what I
// worked out the vendor source means. This one asserts that what I worked out
// is what the vendor source actually does. If it disagrees, the fixture wins.
func TestFrameMatchesTheVendorOracle(t *testing.T) {
	idx, err := os.ReadFile("../../testdata/frame/jd79661-frame.idx")
	if err != nil {
		t.Fatalf("reading the index fixture: %v", err)
	}
	want, err := os.ReadFile("../../testdata/frame/jd79661-frame.bin")
	if err != nil {
		t.Fatalf("reading the packed fixture: %v", err)
	}
	if len(idx) != panelW*panelH {
		t.Fatalf("index fixture is %d bytes, want %d", len(idx), panelW*panelH)
	}
	if len(want) != frameSize {
		t.Fatalf("packed fixture is %d bytes, want %d", len(want), frameSize)
	}

	conn := &recordingConn{}
	d := newDevice(t, conn)

	img := d.NewImage()
	copy(img.Pix, idx) // row-major, stride == width, so a straight copy
	if img.Stride != panelW {
		t.Fatalf("NewImage stride is %d, want %d — the copy above assumes them equal", img.Stride, panelW)
	}

	if err := d.Show(context.Background(), img); err != nil {
		t.Fatalf("Show(): %v", err)
	}
	got := dtmPayload(t, conn.ops)

	if string(got) == string(want) {
		return
	}

	// Locate the disagreement in terms a human can act on, rather than
	// dumping 8,000 bytes.
	diffs := 0
	for i := range want {
		if got[i] != want[i] {
			if diffs < 5 {
				n := i * 4 // first pixel in this byte
				t.Errorf("byte %d: got 0x%02X want 0x%02X — controller (%d,%d), image column %d",
					i, got[i], want[i], n%ctrlW, n/ctrlW, n/ctrlW)
			}
			diffs++
		}
	}
	t.Fatalf("%d of %d bytes differ from the vendor oracle", diffs, len(want))
}

// The same fixture, checked the other way round: unpacking the vendor's bytes
// must give back the picture that went in. This catches a driver and a test
// that are wrong in the same direction — which the test above, comparing two
// things I wrote, cannot.
func TestVendorOracleUnpacksToTheOriginalPicture(t *testing.T) {
	idx, err := os.ReadFile("../../testdata/frame/jd79661-frame.idx")
	if err != nil {
		t.Fatalf("reading the index fixture: %v", err)
	}
	frame, err := os.ReadFile("../../testdata/frame/jd79661-frame.bin")
	if err != nil {
		t.Fatalf("reading the packed fixture: %v", err)
	}

	want := image.NewPaletted(image.Rect(0, 0, panelW, panelH), jd79661.Palette.Colors())
	copy(want.Pix, idx)

	for y := range panelH {
		for x := range panelW {
			// The inverse of the rule in Device.Show.
			cx, cy := panelH-1-y, x
			if v := pixelAt(frame, cy*ctrlW+cx); v != want.ColorIndexAt(x, y) {
				t.Fatalf("image (%d,%d): the vendor frame holds %d, the picture has %d",
					x, y, v, want.ColorIndexAt(x, y))
			}
		}
	}

	// And the padding really is black, not merely off-panel.
	for cy := range ctrlH {
		for cx := panelH; cx < ctrlW; cx++ {
			if v := pixelAt(frame, cy*ctrlW+cx); v != 0 {
				t.Fatalf("padding at controller (%d,%d) is %d, want 0", cx, cy, v)
			}
		}
	}
}

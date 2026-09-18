package e640_test

import (
	"context"
	"os"
	"testing"
)

// The oracle test.
//
// testdata/frame/e640-frame.{idx,bin} is a matched pair captured on the Pi
// from the REAL vendor library, with the real Impression attached: 240,000
// palette indices in, the 120,000 bytes Pimoroni's driver would have sent to
// the panel out. Regenerate with tools/frame_e640.py — see testdata/README.md.
//
// The capture goes through the vendor's set_image(), so this one fixture pins
// all three things that could be silently wrong: the remap that skips wire
// value 4, the quarter-turn rotation, and the nibble order.
func TestFrameMatchesTheVendorOracle(t *testing.T) {
	idx, want := readFixture(t)

	conn := &recordingConn{}
	d := newDevice(t, conn)

	img := d.NewImage()
	if img.Stride != panelW {
		t.Fatalf("NewImage stride is %d, want %d — the copy below assumes them equal", img.Stride, panelW)
	}
	copy(img.Pix, idx)

	if err := d.Show(context.Background(), img); err != nil {
		t.Fatalf("Show(): %v", err)
	}
	got := dtmPayload(t, conn.ops)

	if string(got) == string(want) {
		return
	}
	diffs := 0
	for i := range want {
		if got[i] != want[i] {
			if diffs < 5 {
				n := i * 2 // first pixel in this byte
				t.Errorf("byte %d: got 0x%02X want 0x%02X — controller (%d,%d), image column %d",
					i, got[i], want[i], n%ctrlW, n/ctrlW, n/ctrlW)
			}
			diffs++
		}
	}
	t.Fatalf("%d of %d bytes differ from the vendor oracle", diffs, len(want))
}

// The same fixture the other way round: unpacking the vendor's bytes with the
// rule written out independently here must give back the picture that went
// in. A driver and a test wrong in the same direction still fail this.
func TestVendorOracleUnpacksToTheOriginalPicture(t *testing.T) {
	idx, frame := readFixture(t)

	// Palette index -> wire value, from inky_e640.py set_image(). Written
	// out here rather than taken from the driver, which is the point.
	wire := [...]byte{0, 1, 2, 3, 5, 6}

	for y := range panelH {
		for x := range panelW {
			cx, cy := panelH-1-y, x
			got := nibbleAt(frame, cy*ctrlW+cx)
			if want := wire[idx[y*panelW+x]]; got != want {
				t.Fatalf("image (%d,%d): the vendor frame holds %d, the picture needs wire value %d",
					x, y, got, want)
			}
		}
	}
}

func readFixture(t *testing.T) (idx, frame []byte) {
	t.Helper()
	idx, err := os.ReadFile("../../testdata/frame/e640-frame.idx")
	if err != nil {
		t.Fatalf("reading the index fixture: %v", err)
	}
	frame, err = os.ReadFile("../../testdata/frame/e640-frame.bin")
	if err != nil {
		t.Fatalf("reading the packed fixture: %v", err)
	}
	if len(idx) != panelW*panelH {
		t.Fatalf("index fixture is %d bytes, want %d", len(idx), panelW*panelH)
	}
	if len(frame) != frameSize {
		t.Fatalf("packed fixture is %d bytes, want %d", len(frame), frameSize)
	}
	return idx, frame
}

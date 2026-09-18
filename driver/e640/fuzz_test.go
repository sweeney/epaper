package e640_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/driver/e640"
)

// FuzzFrame throws arbitrary pictures at the E640's frame builder.
//
// This is the one packer in the library that translates as it packs — palette
// positions become wire values that skip 4 — and it walks the image in the
// controller's rotated order. The contract: an image whose every index is in
// the palette produces exactly w*h/2 bytes that unpack, by the rule written
// out independently below, to the right wire value at the right place; any
// other image is refused with ErrBadImage before the transport sees a byte.
// Never a panic, and never a wire value of 4 or above 6.
func FuzzFrame(f *testing.F) {
	f.Add(4, 2, []byte{0, 1, 2, 3, 4, 5, 0, 1})
	f.Add(2, 2, []byte{5, 4, 3, 2})
	f.Add(3, 4, []byte{6, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}) // one index past the palette
	f.Add(1, 2, []byte{255, 0})

	wire := [...]byte{0, 1, 2, 3, 5, 6}

	f.Fuzz(func(t *testing.T, w, h int, pix []byte) {
		// Sane sizes, and an even height: New rejects an odd one, which is
		// tested on its own.
		if w < 1 || h < 2 || w > 64 || h > 64 || h%2 != 0 {
			t.Skip()
		}

		conn := &recordingConn{}
		d, err := e640.New(conn, e640.Config{Width: w, Height: h, MinRefreshTime: -1})
		if err != nil {
			t.Fatalf("New(%dx%d): %v", w, h, err)
		}
		img := d.NewImage()
		copy(img.Pix, pix)

		valid := true
		for _, v := range img.Pix {
			if int(v) >= len(e640.Palette) {
				valid = false
				break
			}
		}

		err = d.Show(context.Background(), img)
		if !valid {
			if !errors.Is(err, epaper.ErrBadImage) {
				t.Fatalf("Show() of an image with an out-of-palette index = %v, want ErrBadImage", err)
			}
			if len(conn.ops) != 0 {
				t.Fatalf("%d operations reached the transport for a rejected image", len(conn.ops))
			}
			return
		}
		if err != nil {
			t.Fatalf("Show(): %v", err)
		}

		frame := dtmPayload(t, conn.ops)
		if len(frame) != w*h/2 {
			t.Fatalf("frame is %d bytes, want %d", len(frame), w*h/2)
		}
		// controller (cx, cy) <- image (x = cy, y = h-1-cx), fast axis h.
		for y := range h {
			for x := range w {
				got := nibbleAt(frame, x*h+(h-1-y))
				if want := wire[img.ColorIndexAt(x, y)]; got != want {
					t.Fatalf("image (%d,%d) sent as %d, want wire value %d", x, y, got, want)
				}
			}
		}
	})
}

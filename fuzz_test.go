package epaper_test

import (
	"image"
	"testing"

	"github.com/sweeney/epaper"
)

// FuzzPack throws arbitrary images at the framebuffer packer.
//
// Pack is the last thing that runs before bytes reach the panel, and the one
// place a bad index becomes a wrong colour on the glass. The contract is that
// it either produces exactly ceil(w*h/4) bytes or refuses — never a panic, and
// never a short buffer, which the controller would read past.
func FuzzPack(f *testing.F) {
	f.Add(4, 1, []byte{0, 1, 2, 3})
	f.Add(2, 2, []byte{3, 2, 1, 0})
	f.Add(3, 2, []byte{1, 1, 1, 1, 1, 1}) // odd pixel count
	f.Add(0, 0, []byte{})
	f.Add(400, 300, make([]byte, 120000))

	f.Fuzz(func(t *testing.T, w, h int, pix []byte) {
		// Keep the sizes sane: this is testing the packing rule, not the
		// allocator.
		if w < 0 || h < 0 || w > 2000 || h > 2000 {
			t.Skip()
		}

		img := image.NewPaletted(image.Rect(0, 0, w, h), fourInk.Colors())
		copy(img.Pix, pix)

		got, err := epaper.Pack(img)
		if err != nil {
			if got != nil {
				t.Fatalf("Pack() returned %d bytes alongside error %v", len(got), err)
			}
			return
		}

		want := (w*h + 3) / 4
		if len(got) != want {
			t.Fatalf("Pack() of %dx%d gave %d bytes, want %d", w, h, len(got), want)
		}

		// Every byte must be four two-bit fields, so no information can have
		// been lost or invented: unpacking has to give the pixels back.
		for i, b := range got {
			for shift := range 4 {
				n := i*4 + shift
				if n >= w*h {
					break
				}
				idx := (b >> (6 - 2*shift)) & 3
				if idx != img.Pix[n] {
					t.Fatalf("pixel %d packed as %d, want %d", n, idx, img.Pix[n])
				}
			}
		}
	})
}

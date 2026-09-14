package epaper_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"os"
	"testing"

	"github.com/sweeney/epaper"
)

func TestPack(t *testing.T) {
	for _, tc := range []struct {
		name string
		w, h int
		pix  []uint8
		want []byte
	}{
		{
			// The documented rule, one byte at a time:
			//   byte = (p0&3)<<6 | (p1&3)<<4 | (p2&3)<<2 | (p3&3)
			name: "one byte, all four inks",
			w:    4, h: 1,
			pix:  []uint8{0, 1, 2, 3},
			want: []byte{0b00_01_10_11},
		},
		{
			name: "most significant pixel comes first",
			w:    4, h: 1,
			pix:  []uint8{3, 0, 0, 0},
			want: []byte{0b11_00_00_00},
		},
		{
			// Packing runs over the FLATTENED stream, so with a width that is
			// not a multiple of four a byte straddles the row boundary. This
			// is what the vendor library does (numpy flatten, then stride).
			name: "packing spans rows when width is not a multiple of 4",
			w:    2, h: 2,
			pix:  []uint8{0, 1, 2, 3},
			want: []byte{0b00_01_10_11},
		},
		{
			// 6 pixels -> 2 bytes, the last one half used. The pad is zeroed
			// and lands outside the panel's addressable area.
			name: "odd pixel count pads the final byte",
			w:    3, h: 2,
			pix:  []uint8{3, 3, 3, 3, 3, 3},
			want: []byte{0xFF, 0b11_11_00_00},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := newPaletted(tc.w, tc.h, tc.pix)
			got, err := epaper.Pack(img)
			if err != nil {
				t.Fatalf("Pack() error: %v", err)
			}
			if !bytes.Equal(got, tc.want) {
				t.Errorf("Pack() = %08b, want %08b", got, tc.want)
			}
		})
	}
}

// A SubImage has Stride != Dx. Reading Pix straight through would silently
// pack the parent image's row padding as pixels.
func TestPackRespectsStride(t *testing.T) {
	// The right-hand half is filler: valid indices, but not the ones the
	// sub-image should produce.
	parent := newPaletted(8, 2, []uint8{
		0, 1, 2, 3, 1, 1, 1, 1,
		3, 2, 1, 0, 1, 1, 1, 1,
	})
	sub, ok := parent.SubImage(image.Rect(0, 0, 4, 2)).(*image.Paletted)
	if !ok {
		t.Fatal("SubImage did not return *image.Paletted")
	}
	if sub.Stride == sub.Rect.Dx() {
		t.Fatal("test is not exercising stride handling: SubImage stride equals its width")
	}

	got, err := epaper.Pack(sub)
	if err != nil {
		t.Fatalf("Pack() error: %v", err)
	}
	want := []byte{0b00_01_10_11, 0b11_10_01_00}
	if !bytes.Equal(got, want) {
		t.Errorf("Pack() = %08b, want %08b — row padding leaked into the framebuffer", got, want)
	}
}

func TestPackErrors(t *testing.T) {
	sixColour := make(color.Palette, 6)
	for i := range sixColour {
		sixColour[i] = color.RGBA{uint8(i), 0, 0, 255}
	}

	for _, tc := range []struct {
		name string
		img  *image.Paletted
		want error
	}{
		{"nil image", nil, epaper.ErrBadImage},
		{"empty image", image.NewPaletted(image.Rect(0, 0, 0, 0), fourInk.Colors()), epaper.ErrBadImage},
		{
			// Masking the index with &3, as the vendor does, would turn
			// index 4 into black and put a wrong colour on the panel.
			name: "palette larger than 2 bits",
			img:  image.NewPaletted(image.Rect(0, 0, 4, 1), sixColour),
			want: epaper.ErrPaletteTooLarge,
		},
		{
			name: "pixel index outside its own palette",
			img: func() *image.Paletted {
				i := newPaletted(4, 1, []uint8{0, 1, 2, 3})
				i.Palette = i.Palette[:2] // indices 2 and 3 now dangle
				return i
			}(),
			want: epaper.ErrBadImage,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := epaper.Pack(tc.img)
			if !errors.Is(err, tc.want) {
				t.Errorf("Pack() error = %v, want %v", err, tc.want)
			}
		})
	}
}

// The panel takes exactly 30,000 bytes. Getting this wrong is a blank screen.
func TestPackFullPanelLength(t *testing.T) {
	img := image.NewPaletted(image.Rect(0, 0, 400, 300), fourInk.Colors())
	got, err := epaper.Pack(img)
	if err != nil {
		t.Fatalf("Pack() error: %v", err)
	}
	if len(got) != 30000 {
		t.Errorf("Pack() length = %d, want 30000", len(got))
	}
}

// The strongest packing test in the repo: the fixture was produced by the
// vendor library on our actual board. If we disagree with it, we are wrong.
func TestPackMatchesConformanceFixture(t *testing.T) {
	idx, err := os.ReadFile("testdata/conformance/conformance.idx")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	want, err := os.ReadFile("testdata/conformance/conformance.bin")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	img := image.NewPaletted(image.Rect(0, 0, 400, 300), fourInk.Colors())
	copy(img.Pix, idx)

	got, err := epaper.Pack(img)
	if err != nil {
		t.Fatalf("Pack() error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("packed framebuffer does not match the vendor oracle (first diff at byte %d)", firstDiff(got, want))
	}
}

func firstDiff(a, b []byte) int {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return min(len(a), len(b))
	}
	return -1
}

func newPaletted(w, h int, pix []uint8) *image.Paletted {
	img := image.NewPaletted(image.Rect(0, 0, w, h), fourInk.Colors())
	copy(img.Pix, pix)
	return img
}

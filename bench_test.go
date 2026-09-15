package epaper_test

import (
	"image"
	"testing"

	"github.com/sweeney/epaper"
)

// Packing is the only thing between a finished image and the SPI bus, and it
// runs on every refresh. It needs to be negligible next to the ~20 s the panel
// takes, on a Pi Zero as well as a Pi 4.
func BenchmarkPack(b *testing.B) {
	img := image.NewPaletted(image.Rect(0, 0, 400, 300), fourInk.Colors())
	for i := range img.Pix {
		img.Pix[i] = uint8(i % 4)
	}

	b.SetBytes(int64(len(img.Pix)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := epaper.Pack(img); err != nil {
			b.Fatal(err)
		}
	}
}

// A sub-image has a stride wider than its width, so packing walks it row by
// row. Worth knowing that costs nothing much.
func BenchmarkPackSubImage(b *testing.B) {
	parent := image.NewPaletted(image.Rect(0, 0, 800, 300), fourInk.Colors())
	sub, ok := parent.SubImage(image.Rect(0, 0, 400, 300)).(*image.Paletted)
	if !ok {
		b.Fatal("SubImage did not return *image.Paletted")
	}

	b.SetBytes(int64(400 * 300))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := epaper.Pack(sub); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPaletteIndex(b *testing.B) {
	for b.Loop() {
		if _, ok := fourInk.Index(epaper.Red); !ok {
			b.Fatal("red is missing")
		}
	}
}

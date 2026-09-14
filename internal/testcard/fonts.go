package testcard

import (
	"fmt"
	"sync"

	"github.com/sweeney/epaper/render"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

// newFamily parses a TrueType font once and hands out cached faces, which is
// what render.FontFamily asks implementations to do.
func newFamily(ttf []byte) (render.FontFamily, error) {
	parsed, err := opentype.Parse(ttf)
	if err != nil {
		return nil, fmt.Errorf("testcard: parsing font: %w", err)
	}
	var mu sync.Mutex
	cache := map[int]font.Face{}
	return func(size int) (font.Face, error) {
		mu.Lock()
		defer mu.Unlock()
		if f, ok := cache[size]; ok {
			return f, nil
		}
		// DPI 72 makes one point equal one pixel, so size means pixels.
		f, err := opentype.NewFace(parsed, &opentype.FaceOptions{
			Size: float64(size), DPI: 72,
			// Hinting OFF is deliberate and makes small text markedly more
			// legible here; see render.FontFamily for the reasoning.
			Hinting: font.HintingNone,
		})
		if err != nil {
			return nil, fmt.Errorf("testcard: face at %dpx: %w", size, err)
		}
		cache[size] = f
		return f, nil
	}, nil
}

package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html/template"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Image is a golden PNG, inlined into the page, optionally paired with the
// actual output from a run where it did not match.
type Image struct {
	Name   string
	Width  int
	Height int
	Bytes  int
	Data   template.URL // a data: URI

	// Actual is set when a <name>.got.png sits beside the golden, which the
	// golden helper writes on a mismatch. Showing the two together is the
	// difference between "the render changed" and being able to see how.
	Actual  template.URL
	Changed bool
}

// maxImageBytes keeps one oversized PNG from producing a report nobody can
// open. The goldens here are a few KB; anything far larger is a mistake.
const maxImageBytes = 4 << 20

// loadImages reads every golden PNG in a directory and inlines it.
//
// A <name>.got.png is not listed on its own: it is the ACTUAL output the
// golden helper writes when <name>.png did not match, so it is attached to
// that golden instead and the page shows the pair. Listing it separately, with
// no indication that it contradicts the image next to it, would be worse than
// leaving it out.
func loadImages(dir string) ([]Image, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	// Actual output first, so it can be attached as the goldens are read.
	actual := map[string]template.URL{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".got.png") {
			continue
		}
		uri, _, err := inline(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		actual[strings.TrimSuffix(e.Name(), ".got.png")] = uri
	}

	var out []Image
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".png") || strings.HasSuffix(e.Name(), ".got.png") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		uri, cfg, err := inline(path)
		if err != nil {
			return nil, err
		}

		name := strings.TrimSuffix(e.Name(), ".png")
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		img := Image{
			Name:   name,
			Width:  cfg.Width,
			Height: cfg.Height,
			Bytes:  int(info.Size()),
			Data:   uri,
		}
		if got, ok := actual[name]; ok {
			img.Actual, img.Changed = got, true
		}
		out = append(out, img)
	}

	sort.Slice(out, func(i, j int) bool {
		// Changed renders first: they are the reason to open the page.
		if out[i].Changed != out[j].Changed {
			return out[i].Changed
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// inline reads a PNG, checks its size and returns it as a data: URI.
func inline(path string) (template.URL, image.Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", image.Config{}, err
	}
	if len(b) > maxImageBytes {
		return "", image.Config{}, fmt.Errorf("%s is %d bytes, over the %d limit",
			filepath.Base(path), len(b), maxImageBytes)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return "", image.Config{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(b)), cfg, nil
}

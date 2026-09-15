package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sampleRun(t *testing.T) *Run {
	t.Helper()
	run, err := parse(strings.NewReader(sampleJSON))
	if err != nil {
		t.Fatal(err)
	}
	run.Title = "epaper"
	run.Branch = "main"
	run.Commit = "abc1234"
	run.Generated = time.Date(2026, 9, 14, 20, 0, 0, 0, time.UTC)
	return run
}

func TestRender(t *testing.T) {
	var buf bytes.Buffer
	if err := render(&buf, sampleRun(t)); err != nil {
		t.Fatalf("render(): %v", err)
	}
	out := buf.String()

	for _, want := range []string{
		"<!doctype html>", "epaper", "FAIL", "TestTwo", "boom",
		"example.com/a", "main", "abc1234",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the page does not mention %q", want)
		}
	}

	// Self-contained is the whole point of a CI artifact: it has to open on
	// a laptop with no network and no shared assets.
	for _, forbidden := range []string{"http://", "https://", "<script"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("the page contains %q, so it is not self-contained", forbidden)
		}
	}
}

// Test output is arbitrary text and goes straight into the page. A test name
// or a failure message containing markup must not be able to break out of it.
func TestRenderEscapesTestOutput(t *testing.T) {
	run := &Run{
		Title:     "t",
		Generated: time.Now(),
		Packages: []*Package{{
			Name:   "example.com/x",
			Status: "fail",
			Tests: []*Test{{
				Name:   "TestXSS",
				Status: "fail",
				Output: `<script>alert("pwned")</script>`,
			}},
		}},
		Failed: 1,
	}

	var buf bytes.Buffer
	if err := render(&buf, run); err != nil {
		t.Fatalf("render(): %v", err)
	}
	out := buf.String()

	if strings.Contains(out, "<script>alert") {
		t.Error("test output was interpolated unescaped")
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Error("the escaped output is missing entirely")
	}
}

func TestRenderPassingRun(t *testing.T) {
	run := &Run{Title: "t", Generated: time.Now(), Passed: 3}
	var buf bytes.Buffer
	if err := render(&buf, run); err != nil {
		t.Fatalf("render(): %v", err)
	}
	if !strings.Contains(buf.String(), ">PASS<") {
		t.Error("a passing run is not badged PASS")
	}
}

// writePNG writes a PNG whose content depends on its name, so that two
// fixtures of the same size are not byte-identical.
func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var sum int
	for _, r := range filepath.Base(path) {
		sum += int(r)
	}
	img.Set(0, 0, color.RGBA{uint8(sum), uint8(sum >> 3), 3, 255})
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestLoadImages(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "testcard.png"), 400, 300)
	writePNG(t, filepath.Join(dir, "primitives.png"), 200, 120)
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatal(err)
	}

	imgs, err := loadImages(dir)
	if err != nil {
		t.Fatalf("loadImages(): %v", err)
	}
	if len(imgs) != 2 {
		t.Fatalf("%d images, want 2 (the .txt excluded)", len(imgs))
	}
	// Sorted, so the page is stable between runs.
	if imgs[0].Name != "primitives" || imgs[1].Name != "testcard" {
		t.Errorf("names = %q, %q; want them sorted", imgs[0].Name, imgs[1].Name)
	}
	if imgs[1].Width != 400 || imgs[1].Height != 300 {
		t.Errorf("testcard = %dx%d, want 400x300", imgs[1].Width, imgs[1].Height)
	}
	if !strings.HasPrefix(string(imgs[0].Data), "data:image/png;base64,") {
		t.Error("images are not inlined as data URIs")
	}
	for _, img := range imgs {
		if img.Changed {
			t.Errorf("%s is marked changed with no .got.png present", img.Name)
		}
	}
}

// A failing golden writes <name>.got.png beside it. That must be attached to
// the golden as a pair, not listed on its own — an unlabelled image sitting
// next to the one it contradicts is worse than leaving it out.
func TestLoadImagesPairsActualOutput(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "testcard.png"), 400, 300)
	writePNG(t, filepath.Join(dir, "testcard.got.png"), 400, 300)
	writePNG(t, filepath.Join(dir, "primitives.png"), 200, 120)

	imgs, err := loadImages(dir)
	if err != nil {
		t.Fatalf("loadImages(): %v", err)
	}
	if len(imgs) != 2 {
		t.Fatalf("%d images, want 2 — the .got.png must not get a row of its own", len(imgs))
	}

	// Changed renders sort first: they are the reason to open the page.
	if imgs[0].Name != "testcard" {
		t.Errorf("first image is %q, want the changed testcard", imgs[0].Name)
	}
	if !imgs[0].Changed {
		t.Error("testcard has a .got.png but is not marked changed")
	}
	if !strings.HasPrefix(string(imgs[0].Actual), "data:image/png;base64,") {
		t.Error("the actual output was not inlined")
	}
	if imgs[0].Actual == imgs[0].Data {
		t.Error("expected and actual are the same data URI")
	}
	if imgs[1].Changed {
		t.Error("primitives has no .got.png but is marked changed")
	}
}

func TestLoadImagesErrors(t *testing.T) {
	if _, err := loadImages(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("loadImages() on a missing directory = nil error")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.png"), []byte("not a png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadImages(dir); err == nil {
		t.Error("loadImages() on an undecodable PNG = nil error")
	}
}

// The images end up inline, so one oversized file makes a report nobody can
// open. Better to refuse it and say why.
func TestLoadImagesRejectsHugeFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "huge.png"), make([]byte, maxImageBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadImages(dir); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("loadImages() on an oversized file = %v, want a size-limit error", err)
	}
}

func TestWriteSummary(t *testing.T) {
	var buf bytes.Buffer
	if err := writeSummary(&buf, sampleRun(t), "test-report"); err != nil {
		t.Fatalf("writeSummary(): %v", err)
	}
	out := buf.String()

	for _, want := range []string{
		"FAIL", "### Failures", "TestTwo", "boom",
		"Coverage by package", "test-report",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the summary does not mention %q", want)
		}
	}
	// The module prefix on every row would push the useful part off the edge.
	if strings.Contains(out, "github.com/sweeney/epaper/") {
		t.Error("package names were not shortened")
	}
}

func TestWriteSummaryPassing(t *testing.T) {
	var buf bytes.Buffer
	run := &Run{Title: "t", Passed: 5, Generated: time.Now()}
	if err := writeSummary(&buf, run, ""); err != nil {
		t.Fatalf("writeSummary(): %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "PASS") {
		t.Error("a passing run is not reported as PASS")
	}
	if strings.Contains(out, "### Failures") {
		t.Error("a passing run should have no Failures section")
	}
}

func TestShortPath(t *testing.T) {
	for in, want := range map[string]string{
		"github.com/sweeney/epaper":        "epaper",
		"github.com/sweeney/epaper/render": "render",
		"example.com/other":                "example.com/other",
	} {
		if got := shortPath(in); got != want {
			t.Errorf("shortPath(%q) = %q, want %q", in, got, want)
		}
	}
}

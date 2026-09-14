# epaper — plan

A Go library for driving e-ink panels from a Raspberry Pi.

**Status:** planning. No code written yet. This document is the contract for
what gets built and how.

**Picking this up cold?** Read §1 for the goal, §2 for the hardware facts, §4
for the API being committed to — then §11 for how to reach the bench, what
assets already exist, and what must not be deleted.

Then read **`testdata/README.md`**. The fixtures are already captured from real
hardware, and that file documents the exact dither matrix, packing rule and
failure cases they encode. Milestones M1–M4 and M6 can all be completed against
those fixtures with **no Pi in the room**.

---

## 1. Goal, in one sentence

Make the panel disappear, so that a program that wants to put something on an
e-ink screen thinks only about the picture.

Everything below serves that. If a decision makes the consumer's life harder in
order to make the library's internals tidier, it is the wrong decision.

### 1.1 What this is

- A driver for e-ink panels attached over SPI, with GPIO for reset/data-command/busy.
- A small render toolkit for the drawing primitives that e-ink actually needs.
- A mock implementation good enough that the entire consumer-facing surface can
  be exercised with no hardware present.

### 1.2 What this is not

- **Not a UI framework.** No widgets, no layout engine, no reactive anything.
- **Not a service.** No scheduling, no data fetching, no daemon. A consumer
  builds that on top; the library is imported, never run.
- **Not a general image pipeline.** No photograph quantisation (see §4.4). If
  you want to put a JPEG on a four-colour panel, that is your problem to solve
  and we will happily take the resulting `*image.Paletted`.
- **Not a vendor abstraction layer.** See §3.

### 1.3 Non-negotiables

These come from things that actually bit us, or from watching the Python
library misbehave on the bench. Each is a hard requirement, not a preference.

| Rule | Why |
|---|---|
| **Never call `os.Exit`, never `panic` on hardware state** | Pimoroni's `gpiodevice` calls `sys.exit(1)` when a pin is busy. Our `try/except` caught nothing — the process simply died. A library that kills its caller cannot be used in a service |
| **Every failure is a returned `error`** | Same reason. Sentinel errors so callers can `errors.Is` and act |
| **No cgo** | `GOOS=linux GOARCH=arm64 go build` must produce a static binary, cross-compiled from a Mac, with no toolchain on the Pi |
| **No root required** | A user in `spi`, `i2c` and `gpio` can drive the panel. Verified on the bench. The library must not need more |
| **`context.Context` on anything that blocks** | A refresh takes ~25 s. That is far too long to be uncancellable |
| **Measure, don't inherit** | Where we copy a magic constant from Pimoroni, say so in a comment and cite it. Where we copy a *delay*, test whether it is load-bearing first (§9.3) |

---

## 2. Hardware facts established on the bench

Verified on a Pi 4B / Debian 13 trixie with an Inky wHAT 4.2", 2026-09-14.
These are measurements, not assumptions, and the driver is written against them.

### 2.1 The panel

| Property | Value |
|---|---|
| Resolution | 400 × 300 |
| Controller | **JD79668** |
| Inks | **four**: `BLACK=0 WHITE=1 YELLOW=2 RED=3`, simultaneously |
| Full refresh | **25.4 s** measured, repeatable |
| Effective resolution | genuinely 400 × 300 — 1px rules at 2/3/4/5px pitch and 1/2/4px checkerboards all render cleanly |
| Smallest readable text | **10px**. 8px is not readable |
| Waveform LUTs | **none to upload** — `self._luts = None`; the waveform is in panel OTP |

The four-colour finding matters: the board's own handover notes claimed three
colours with a single accent. They were wrong. `colour == "red/yellow"` in the
EEPROM means *both*, not *either*.

### 2.2 Wiring (Pimoroni HAT)

```
RESET  GPIO 27      BUSY  GPIO 17      DC  GPIO 22      CS  GPIO 8
SPI    /dev/spidev0.0, mode 0, 1 MHz
I2C    /dev/i2c-1, EEPROM at 0x50
```

`dtoverlay=spi0-0cs` **is required**. Without it the kernel SPI driver owns
GPIO 8 and the panel cannot be addressed:

```
Chip Select: (line 8, GPIO8) currently claimed by spi0 CS0
```

With the overlay set, only `/dev/spidev0.0` exists (`spidev0.1` disappears) and
**chip-select is asserted by us as a GPIO**, active low.

### 2.3 Signal semantics

- **CS** — active low. Assert (low) before a transfer, release (high) after.
- **DC** — low = command, high = data.
- **RESET** — active low pulse: low, 30 ms, high, 30 ms.
- **BUSY** — **low while busy, high when ready**, with a host pull-up.
  The pull-up means a *disconnected* BUSY line reads "ready". Pimoroni handle
  this by sleeping the full 40 s timeout. **We will return an error instead** —
  silently sleeping 40 s is worse than failing.

### 2.4 Wire format

Four pixels per byte, two bits each, most-significant first:

```
byte = (p0&3)<<6 | (p1&3)<<4 | (p2&3)<<2 | (p3&3)
```

400 × 300 = 120,000 px → **30,000 bytes**. The spidev `bufsiz` default is
4096, so writes must be chunked. CS stays asserted across chunks.

### 2.5 Command sequence

Init (after reset), payloads verbatim from the vendor driver:

```
0x4D [0x78]
0x00 [0x0F,0x29]                          PSR
0x06 [0x0D,0x12,0x24,0x25,0x12,0x29,0x10] BTST_P
0x30 [0x08]
0x50 [0x37]                               CDI
0x61 [0x01,0x90,0x01,0x2C]                TRES  = 400 x 300
0xAE [0xCF]   0xB0 [0x13]   0xBD [0x07]   0xBE [0xFE]   0xE9 [0x01]
```

Refresh:

```
0x10 <30000 bytes>   DTM   framebuffer
0x04                 PON   power on      -> wait BUSY
0x12 [0x00]          DRF   refresh       -> wait BUSY
0x02 [0x00]          POF   power off     -> wait BUSY
0x07 [0xA5]          DSLP  deep sleep
```

Init runs before **every** refresh, because `DSLP` puts the panel to sleep at
the end of the previous one.

### 2.6 EEPROM

29 bytes at address `0x50`, **two-byte register addressing** (write `0x0000`,
repeated start, read 29). Byte-mode SMBus reads return garbage — `i2cdump -b`
gave us `01 ff ff…` and then `30`/`31` on successive runs, which is what sent
us chasing a non-existent seating fault.

Layout (little-endian, `<HHBBB22p`):

```
u16 width | u16 height | u8 colour | u8 pcbVariant | u8 displayVariant | pascal[22] writeTime
```

`displayVariant == 24` → `"Red/Yellow wHAT (JD79668)"`.

---

## 3. Scope and the "other screens" question

The controllers are not made by the board vendors. JD79668, SSD1683, SSD1608
and UC8159 come from Solomon Systech, Fitipower and others, and every vendor
rebadges them. A JD79668 driver will drive *any* board carrying that
controller; only the pin map and the detection mechanism differ.

Pimoroni's own library concedes this: it is organised as `inky_jd79668.py`,
`inky_ssd1683.py`, `inky_uc8159.py` — by controller. Only `auto.py` and
`eeprom.py` are vendor-specific.

**So the split is by controller, with a thin board package on top.** This is
not speculative generality; it is the shape the hardware already has.

### 3.1 What we build now

- **one** controller driver: JD79668
- **one** board package: `inky`, holding the EEPROM detection and pin map
- **no** driver registry, **no** capability negotiation, **no** vendor
  interface beyond `Device`

### 3.2 What we deliberately do not build

Adding an SSD1683 later should mean writing `driver/ssd1683` and one line in
`inky.Open`'s switch. If that turns out to need more, *then* we abstract, with
two real implementations in hand to abstract *from*.

We will document the extension path in `CONTRIBUTING.md` without building it.

### 3.3 Names are load-bearing

Import paths are the one thing that is genuinely painful to change once
anything depends on them. Hence `epaper` (neutral) with `epaper/inky`
(vendor), rather than `inky` with a future awkward rename.

---

## 4. The consumer API

This is the part that matters most. Everything else is implementation.

### 4.1 The whole thing, end to end

```go
package main

import (
	"context"
	"image"
	"log"
	"time"

	"github.com/sweeney/epaper"
	"github.com/sweeney/epaper/inky"
	"github.com/sweeney/epaper/render"
)

func main() {
	dev, err := inky.Open()
	if err != nil {
		log.Fatal(err)
	}
	defer dev.Close()

	face := loadFont() // caller's choice; the library embeds none

	c := render.NewCanvasFor(dev) // bounds and palette both come from the device
	c.Fill(epaper.White)
	c.Rect(image.Rect(0, 0, 400, 30), epaper.Red)
	c.Text(image.Pt(8, 6), "HELLO", face, epaper.White)
	if err := c.Err(); err != nil {
		log.Fatal(err) // e.g. this panel has no red
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if err := dev.Show(ctx, c.Image()); err != nil {
		log.Fatal(err)
	}
}
```

Six lines of setup, and nothing in it mentions SPI, GPIO, chip-select, bit
packing or refresh sequencing. That is the bar.

Note what the consumer never writes: a palette index, an image size, or a
colour model. `NewCanvasFor(dev)` takes all three from the panel.

### 4.2 `epaper.Device`

```go
type Device interface {
	Model() string
	Bounds() image.Rectangle
	Palette() Palette
	NewImage() *image.Paletted
	Show(ctx context.Context, img *image.Paletted) error
	Close() error
}
```

Design notes, each with a reason:

- **`*image.Paletted`, not `image.Image`.** Explicit. The panel has four inks
  and the caller should know it. Accepting `image.Image` would force us to
  ship a quantiser (§4.4), and silently dithering someone's careful layout is
  a bad surprise. `NewImage()` hands back a correctly sized, correctly
  palettised canvas so this costs the caller nothing.
- **`Show` takes a context** and returns when the refresh is complete.
  Cancelling abandons the *wait*; the panel finishes its own refresh
  regardless — documented explicitly, because it is surprising.
- **`Model() string`** rather than an enum. It comes from the EEPROM as text;
  inventing a closed enum for a list we do not control is a liability.
- **`Palette()` is on the device, not the package** (§4.3). Panels differ in
  what inks they have, so the capability has to travel with the panel. `Show`
  validates that the image's palette matches the device's and returns
  `ErrPaletteMismatch` rather than sending pixels the controller will
  misinterpret.
- **No `SetPixel`.** That is the image's job, not the device's.
- **No `Sleep`/`Wake`.** The controller deep-sleeps after every refresh and
  re-inits before the next one. Exposing power management would be exposing an
  implementation detail that has no meaningful states for a caller to choose
  between.

### 4.3 Inks and palettes

Panels differ in what they can display, and this is the first thing the API has
to get right. A mono panel has two inks; our wHAT has four; Spectra 6 has six;
the UC8159 seven. Worse, "red" on a three-colour wHAT is a visibly different
red from Spectra's. A package-level `Red` constant would be baking one panel's
capabilities into the whole library.

So two separate concepts, which an earlier draft of this plan wrongly conflated:

```go
// Ink is WHICH COLOUR YOU MEAN. Universal, panel-independent.
type Ink uint8

const (
	Black Ink = iota
	White
	Red
	Yellow
	Green
	Blue
	Orange
)

// Palette is WHAT A GIVEN PANEL CAN DO. Slice position is the palette index
// the controller is sent, so order is wire-significant.
type Palette []Entry

type Entry struct {
	Ink Ink
	RGB color.RGBA // this panel's rendition, for previews and PNG output
}

func (p Palette) Index(ink Ink) (uint8, bool) // resolve, or report absent
func (p Palette) Has(ink Ink) bool
func (p Palette) Colors() color.Palette       // for image.Paletted
```

A driver declares its own:

```go
// driver/jd79668
var Palette = epaper.Palette{
	{epaper.Black,  color.RGBA{0, 0, 0, 255}},
	{epaper.White,  color.RGBA{255, 255, 255, 255}},
	{epaper.Yellow, color.RGBA{235, 205, 40, 255}},
	{epaper.Red,    color.RGBA{190, 45, 40, 255}},
}
```

Consumers name inks and never touch indices:

```go
c.Fill(epaper.White)
c.Rect(header, epaper.Red)
```

**What happens when an ink is not available?** Drawing code that returns an
error on every call is miserable to write, so `Canvas` carries a sticky error
in the style of `bufio.Writer`:

```go
c.Fill(epaper.White)
c.Rect(r, epaper.Green)   // not on a four-colour panel
c.Text(p, "hi", f, epaper.Black)

if err := c.Err(); err != nil {
	// "render: ink green not available on this palette"
}
```

The first failure is recorded and subsequent draws are no-ops, so one check at
the end catches it. **No silent substitution** — quietly swapping green for
black would produce a panel that looks plausible and is wrong, which is the
worst outcome on a display nobody watches.

Callers who *want* a fallback ask for one explicitly:

```go
ink := dev.Palette().NearestTo(epaper.Green) // documented as lossy
```

### 4.4 No quantiser, and why that is fine

The vendor library reaches for Floyd–Steinberg only when handed a non-paletted
image. Our test cards draw directly in palette indices, so the quantiser is
never engaged.

Drawing rather than photographing is also the *right* thing to do on a
four-colour panel — as the bench work showed, the panel's strength is crisp 1px
detail and flat colour, not tonal reproduction.

If a consumer genuinely needs a photograph, `render` can grow
`render.Quantize(image.Image, color.Palette) *image.Paletted` later. It is not
in v1 and it is not in the driver.

### 4.5 `render`

Pure functions over `*image.Paletted`. No hardware, no I/O, fully testable on a
laptop. Contents driven by what the bench work actually needed:

`render` imports `epaper` for `Ink` and `Palette`; the dependency never runs the
other way, so the driver stays free of drawing code.

A `Canvas` is constructed **against a palette**, which is what lets it resolve
inks and report an unavailable one:

```go
package render // import "github.com/sweeney/epaper/render"

type Canvas struct{ ... }

func NewCanvas(r image.Rectangle, p epaper.Palette) *Canvas
func NewCanvasFor(d epaper.Device) *Canvas   // the common case

func (c *Canvas) Image() *image.Paletted
func (c *Canvas) Err() error                 // sticky; see 4.3

func (c *Canvas) Fill(ink epaper.Ink)
func (c *Canvas) Set(x, y int, ink epaper.Ink)
func (c *Canvas) Rect(r image.Rectangle, ink epaper.Ink)
func (c *Canvas) Line(a, b image.Point, ink epaper.Ink)

// Extending the palette by mixing — proven to work at 1px on this panel.
func (c *Canvas) Dither(r image.Rectangle, ink, bg epaper.Ink, ratio float64)
func (c *Canvas) Checker(r image.Rectangle, a, b epaper.Ink, cell int)

// Text, with the measuring behaviour that clipping bugs taught us to want.
func (c *Canvas) Text(p image.Point, s string, f font.Face, ink epaper.Ink)
func (c *Canvas) TextFitted(r image.Rectangle, s string, ff FontFamily, ink epaper.Ink) int
func MeasureText(s string, f font.Face) int
```

`NewCanvasFor(dev)` is the call almost everyone writes: it takes the bounds and
the palette from the device, so the two can never disagree.

`TextFitted` shrinks until the string fits and returns the size used. This
exists because **every layout bug found on the bench was silent clipping** —
"RED/YELLOW" losing its W, the 24px row cut at `01234567`. On e-ink there is no
scrollbar and no overflow indicator; text that does not fit simply vanishes.
The library should make that failure hard to commit.

Fonts are supplied by the caller as `font.Face`. We embed none — a font would
dwarf the rest of the library and the choice is the consumer's.

### 4.6 `epaper/mock`

```go
func New(w, h int, p epaper.Palette) *Device  // implements epaper.Device
func NewLike(d epaper.Device) *Device         // same bounds and palette as a real one
func (d *Device) Frames() []*image.Paletted   // everything Show received
func (d *Device) Last() *image.Paletted
func (d *Device) SavePNG(path string) error
func (d *Device) FailNextShow(err error)      // exercise consumer error paths
```

A consumer can build and test their entire rendering against this with no Pi
in the room. So can we.

---

## 5. Package layout

```
epaper/
  doc.go                  package docs, the worked example from §4.1
  device.go               Device interface, sentinel errors
  ink.go                  Ink identities                          [pure]
  palette.go              Palette, Entry, Index/Has/NearestTo     [pure]
  framebuffer.go          image.Paletted -> packed 2bpp bytes   [pure]
  render/                 Canvas and drawing primitives          [pure]
  mock/                   in-memory Device                       [pure]
  driver/jd79668/         the controller driver
  inky/                   EEPROM detect + Pimoroni pin map -> Open()
  internal/spidev/        /dev/spidev ioctl transport
  internal/i2c/           /dev/i2c ioctl transport
  internal/gpiocdev/      GPIO character device lines
  cmd/epaper-testcard/    Test Card F, as a demo and hardware acceptance test
  testdata/golden/        reference PNGs
```

Rules:

- **`internal/` for transports.** They are implementation. Keeping them
  unexported means we can change them without a major version.
- **Everything marked `[pure]` has no hardware dependency and must be 100%
  unit tested.** That is most of the library by volume.
- **`driver/jd79668` depends on interfaces, not on `internal/spidev`.** See §6.2
  — this is what makes the driver testable.

### 5.1 Dependencies

| Module | Why | Risk |
|---|---|---|
| `golang.org/x/sys` | ioctl | Go team, about as stable as it gets |
| `golang.org/x/image` | `font.Face` for text | Go team |
| `github.com/warthog618/go-gpiocdev` | GPIO chardev | Third party, single maintainer. **Isolated behind `internal/gpiocdev` so it can be swapped or inlined** |

No other dependencies. Any addition needs a justification in the PR.

---

## 6. Testing strategy

Red-green TDD throughout: the failing test is written and seen to fail before
the implementation exists.

### 6.1 The layers, and how each is tested

| Layer | How | Hardware? |
|---|---|---|
| `colour`, `framebuffer` | table-driven unit tests, property tests for round-tripping | no |
| EEPROM parsing | table-driven, **including the real 29 bytes captured from our board** | no |
| `render` | golden PNGs in `testdata/golden` | no |
| `mock` | it *is* test infrastructure; tested via the consumer examples | no |
| `driver/jd79668` | **recording fake transport** — assert the exact byte sequence | no |
| `internal/*` transports | thin; integration tested on the Pi only | yes |
| End to end | Test Card F rendered from Go, compared against the Python output | yes |

### 6.2 The key decision: a recordable transport

The driver must not depend on a concrete SPI type. It depends on:

```go
type Conn interface {
	Command(cmd byte, data []byte) error
	Data(b []byte) error
	Reset(ctx context.Context) error
	WaitReady(ctx context.Context, timeout time.Duration) error
}
```

Which means a test can do this, with no Pi:

```go
rec := &recordingConn{}
d, _ := jd79668.New(rec, 400, 300)
d.Show(ctx, img)

want := []op{
	{kind: reset},
	{kind: cmd, cmd: 0x4D, data: []byte{0x78}},
	{kind: cmd, cmd: 0x00, data: []byte{0x0F, 0x29}},
	// ... the full init sequence ...
	{kind: cmd, cmd: 0x10, data: frame},
	{kind: cmd, cmd: 0x04},
	{kind: waitReady},
	// ...
}
```

**This is the single most valuable test in the repo.** It pins the exact
command sequence — the part where a silent mistake produces a blank panel and a
very bad afternoon — and it runs in microseconds in CI.

### 6.3 Golden images

`render` tests draw and compare against PNGs in `testdata/golden`. Regenerate
with `go test ./render -update`.

Justification from the bench: the `--png` mode on the Python test card caught
**four** layout bugs (clipped header, clipped 24px row, footer overlapping the
checkerboard, dithered ramps drawn entirely off-screen) at roughly 50 ms each.
Finding those on hardware would have cost 25 seconds per attempt plus a walk to
the other room. Golden images make that the default way of working.

### 6.4 Hardware tests

Tagged, so they never run in CI:

```go
//go:build hardware
```

Driven from the Makefile by cross-compiling a test binary, shipping it, and
running it on the Pi:

```
make test-hw HOST=sweeney@192.168.1.6
```

Note for that Makefile: **`/usr/sbin` is not on `PATH` over non-interactive
ssh**. `i2cdetect` and friends must be called by absolute path. This cost us a
confusing false failure on the bench.

### 6.5 Coverage

Target ≥ 90% on the pure packages. No target on `internal/*` transports — they
are thin ioctl wrappers whose real test is the hardware run, and chasing
coverage there would mean mocking the kernel.

---

## 7. CI

GitHub Actions, `.github/workflows/ci.yml`:

| Job | Does |
|---|---|
| `test` | `go test -race -coverprofile ./...` on ubuntu-latest |
| `build` | cross-compile `GOOS=linux GOARCH=arm64` and `GOARCH=arm` (Pi Zero/3 32-bit), plus `darwin/arm64` so the mock path stays portable |
| `lint` | `golangci-lint` — `govet`, `staticcheck`, `errcheck`, `revive` |
| `tidy` | `go mod tidy` produces no diff |
| `docs` | every exported symbol has a doc comment (revive `exported` rule) |

Hardware tests are `//go:build hardware` and never run in CI.

Branch protection: CI green before merge.

---

## 8. Milestones

Each is a PR. Each starts with failing tests.

### M0 — scaffolding
`go.mod`, `Makefile` (targets in §11.5), CI workflow, linter config, `doc.go`,
README skeleton, and the licence/attribution obligations in **§11.7**.
**Done when:** CI is green on an empty library and Pimoroni are credited.

### M1 — inks, palettes, framebuffer  *(pure, no hardware)*
`Ink`, `Palette` with `Index`/`Has`/`NearestTo`/`Colors`, and `pack()` from
`*image.Paletted` to 2bpp bytes.

Tests: table-driven palette resolution including **absent inks**; a test that
pins JD79668's palette order (see §9.1); packing tests covering odd widths and
the exact 30,000-byte length.
**Done when:** a known 400×300 image packs to bytes we have verified by hand,
and asking a four-colour palette for green returns `false` rather than a
plausible-looking index.

### M2 — EEPROM parsing  *(pure)*
`parseEEPROM([]byte) (*EEPROM, error)`. The fixture is already captured and
recorded in **§11.4** — real bytes from our board, no hardware needed to write
this milestone. Test truncated, zeroed and out-of-range-variant inputs too.
**Done when:** §11.4's bytes decode to `Red/Yellow wHAT (JD79668)`, 400×300,
`red/yellow`, pcb 10.0 — and a short read returns an error rather than a
zero-valued struct.

### M3 — mock device + golden infrastructure
`mock.Device`, PNG helpers, `-update` flag.
**Done when:** a test can draw and assert against a golden with no hardware.

### M4 — render
`Canvas`, fills, rects, lines, `Dither`, `Checker`, text with `TextFitted`.
All golden-tested.
**Done when:** Test Card F renders to a golden PNG, entirely on the Mac.

### M5 — transports  *(hardware)*
`internal/spidev`, `internal/i2c`, `internal/gpiocdev`. Thin, with hardware
tests.
**Done when:** `make test-hw` reads our EEPROM and returns the right struct.

### M6 — the driver
`driver/jd79668` against the `Conn` interface, with the full command-sequence
test from §6.2.
**Done when:** the recorded sequence matches §2.5 byte for byte.

### M7 — `inky.Open()`
Wiring: detect, map pins, construct. Friendly errors — in particular
`ErrChipSelectBusy` must say *"add `dtoverlay=spi0-0cs` and reboot"*, because
that is a 20-minute debugging session for anyone who has not met it.
**Done when:** `inky.Open()` returns a working device on the Pi.

### M8 — conformance and hardware acceptance

**Correction to an earlier draft of this plan:** it proposed byte-comparing a
Test Card F render against the Python one. That is not achievable — Pillow and
`x/image/font` rasterise glyphs differently, so anything containing text will
differ by a pixel here and there no matter how correct both are.

So the acceptance test is split:

1. **Conformance, byte-exact, no hardware.** Reproduce
   `testdata/conformance/conformance.idx` and `.bin` exactly. The fixture is
   deliberately font-free — pure geometry, dither and fills — and was generated
   by the vendor library on our board. Three checks in order (render, pack,
   end-to-end) per `testdata/README.md` §2.
2. **Text, against our own goldens.** `testdata/golden/`, generated by our Go
   `render` package and reviewed by eye.
3. **On the panel.** Draw the conformance pattern and Test Card F, look at them.

**Done when:** (1) matches byte for byte, and (3) looks right.

Note (1) needs no Pi and no vendor library — the fixtures are committed. The
Python venv (§11.3) is only needed to *regenerate* them.

### M9 — documentation
Package docs, runnable `Example` functions, README with the §4.1 example,
`CONTRIBUTING.md` with the "how to add a controller" walkthrough.

---

## 9. Open questions

Recorded rather than guessed. Each needs a decision before the milestone that
depends on it.

1. ~~Is `Colour` package-level or per-device?~~ **Resolved (§4.3).** Split into
   `Ink` (universal identity, package-level) and `Palette` (per-device
   capability, declared by the driver). An earlier draft of this plan had a
   package-level `Red` constant, which would have baked one panel's
   capabilities into the whole library.

   Consequence to hold onto: **`Palette` order is wire-significant** — slice
   position *is* the index sent to the controller. A driver author reordering
   the palette for tidiness would silently swap the panel's colours. This needs
   a prominent comment on the type and a test that pins JD79668's order.

2. **Does `Show` re-init every time?** The vendor driver does, because it
   `DSLP`s at the end. Keep that behaviour for M6; revisit only with evidence.

3. **Are the 300 ms per-command delays load-bearing?** The vendor driver sleeps
   300 ms before *every* command. 16 commands ≈ **4.8 s of the 25.4 s refresh**.
   *This is arithmetic from source, not a measurement.* Experiment: patch the
   vendor driver's sleep to 10 ms, time a refresh, inspect the panel. If output
   is clean at ~20 s, our driver omits the blanket delay and keeps only the
   documented reset timings. If it corrupts, we keep the delays and write down
   *why*. **Do this before M6.**

4. **Chunk size.** 4096 is spidev's default `bufsiz`, but it is a module
   parameter. Read `/sys/module/spidev/parameters/bufsiz` at open and use it,
   rather than hard-coding? Probably yes — costs nothing.

5. **Do we expose partial refresh?** JD79668 may support it. We have not tested
   it, no consumer has asked, and it complicates the API. Out of scope for v1;
   revisit if a real use case appears.

6. **`Device.Show` concurrency.** Documented as not safe for concurrent use.
   Should we add a mutex and make it safe, or keep it documented-only? Leaning
   mutex — it is two lines and removes a whole class of consumer bug.

---

## 10. Appendix: things that cost us time on the bench

Recorded so nobody repeats them.

- **`i2cdump -b` lies about this EEPROM.** It uses byte-mode SMBus reads; the
  part needs a two-byte address write first. The garbage it returned looked
  like a seating fault. Always use a proper combined write-then-read.
- **`grep -q` inside a pipeline under `set -o pipefail` inverts its result.**
  `grep -q` exits on first match, closing the pipe; the upstream command takes
  SIGPIPE and exits 141, and `pipefail` reports *that*. A successful match
  reads as a failure. This produced a false "nothing at 0x50 — re-seat the HAT"
  against perfectly good hardware.
- **`/usr/sbin` is not on `PATH` over non-interactive ssh.** `i2cdetect` works
  by hand and vanishes in a script.
- **`dtoverlay=spi0-0cs` is required**, and its absence presents as a
  chip-select conflict, not as a missing device.
- **Silent clipping is the characteristic e-ink layout bug.** No overflow
  indicator, no scrollbar. Measure text; never assume widths.

---

## 11. Handover: environment and assets

Everything needed to pick this up cold.

### 11.1 The bench

```
host    pi4b — 192.168.1.6 (wifi), tailnet 100.89.53.59
ssh     sweeney@192.168.1.6   (key auth works; sudo needs a password)
board   Raspberry Pi 4B Rev 1.5, Debian 13 trixie, kernel 6.18.34, aarch64
panel   Inky wHAT 4.2", JD79668, on the GPIO header
```

**The host is already configured.** SPI, I2C and `dtoverlay=spi0-0cs` are
enabled, `i2c-tools` is installed, and `sweeney` is in `spi`, `i2c` and `gpio`.
No further privileged setup is needed to develop against it.

Do **not** expect `sudo` to work non-interactively. Anything privileged needs a
human at a TTY.

### 11.2 Assets in the setup repo

`~/src/github.com/sweeney/scratch/inky-setup` holds the bring-up work:

| File | Use to us now |
|---|---|
| `verify.sh` | Confirms the hardware path end to end. Run it first if anything looks wrong |
| `testcard.py` | Diagnostic card: refresh timing, legibility ladder, 1px rules, dither ramps |
| `testcard_f.py` | **Test Card F in four inks.** The reference render for M4's golden image, and the visual target for M8 |
| `testcard_f.jpg` | The original BBC card, for comparison |
| `README.md` | Full bring-up narrative and the gotchas in §10 |

Both Python cards support `--png out.png`, so they run on a Mac with no
hardware and produce reference images directly.

`testcard_f.py` is also copied into this repo as
`tools/testcard_f_reference.py`, so the visual target travels with the code.

### 11.3 The Python reference is a live asset — do not delete it

A working Pimoroni install lives at `~/inky-trial/venv` on the Pi
(`inky==2.5.0`, `pillow==12.3.0`, `numpy==2.5.3`).

**It is our known-good oracle for M8** and must survive until that milestone
passes. `~/pi-inky-setup/teardown.sh` removes it; do not run that until the Go
driver is byte-identical and proven on the panel.

The vendor source, which is the authority for every constant in §2.5, is at:

```
~/inky-trial/venv/lib/python3.13/site-packages/inky/inky_jd79668.py
~/inky-trial/venv/lib/python3.13/site-packages/inky/eeprom.py
```

### 11.4 Real EEPROM bytes, captured 2026-09-14

The M2 test fixture. Read from our board with a proper two-byte-address
combined read:

```go
// testdata: our Inky wHAT 4.2", EEPROM written 2025-08-20
var boardEEPROM = []byte{
	0x90, 0x01, // width  = 400
	0x2C, 0x01, // height = 300
	0x07,       // colour = 7 -> "red/yellow"
	0x64,       // pcbVariant = 100 -> v10.0
	0x18,       // displayVariant = 24 -> "Red/Yellow wHAT (JD79668)"
	0x15,       // pascal length = 21
	'2', '0', '2', '5', '-', '0', '8', '-', '2', '0', ' ',
	'1', '5', ':', '5', '1', ':', '5', '5', '.', '5',
}
```

Decodes to: `400x300, colour=red/yellow, pcb=10.0, variant=Red/Yellow wHAT (JD79668)`.

Note `pcbVariant` is stored times ten. An earlier draft of the parser divided
by 10 without saying why; it is doing so because the EEPROM holds `100` for
board revision 10.0.

### 11.5 The development loop

```
make test          # pure packages, on the Mac, fast
make golden        # regenerate testdata/golden
make build         # cross-compile linux/arm64
make test-hw HOST=sweeney@192.168.1.6    # ship a test binary and run on the Pi
make testcard HOST=sweeney@192.168.1.6   # draw Test Card F on the real panel
```

`test-hw` builds with `go test -c -tags hardware`, `scp`s the binary, runs it,
and removes it. No Go toolchain is needed on the Pi.

Remember `/usr/sbin` is absent from `PATH` over non-interactive ssh — any
Makefile recipe calling `i2cdetect` must use the absolute path.

### 11.6 Fixtures: what is committed, and how to use it

Everything needed for byte-exact verification is **already captured and
committed**. No hardware is required to reach M8's conformance check.

```
testdata/eeprom/        7 records: the real board plus six failure modes
testdata/conformance/   .idx (120000 raw indices) .bin (30000 packed) .png
reference/vendor-inky/  the Pimoroni source our constants derive from
tools/                  scripts to regenerate the above
```

**`testdata/README.md` is the important document here.** It gives the exact
dither matrix and threshold rule, the packing expression, what each band of the
conformance pattern tests, and why each EEPROM edge case exists.

Two things worth knowing before you start:

- The packing rule in §2.4 has been **verified**, not merely transcribed:
  re-packing `conformance.idx` reproduces `conformance.bin` exactly.
- The conformance pattern contains **four single-pixel corner markers** at
  known positions. They exist to catch off-by-one, row/column transposition and
  flip errors — bugs that otherwise produce an image that looks broadly fine.

### 11.7 Licence and attribution

The library is original code, but **the JD79668 command sequence, its payload
constants, the EEPROM layout and the display-variant table are derived from
Pimoroni's `inky` library**, which is MIT licensed.

Obligations, to be discharged in M0:

- Ship a `LICENSE` (MIT, to match and to keep things simple).
- Add `NOTICE` or a clear README section crediting Pimoroni, naming the `inky`
  project and its MIT licence.
- In `driver/jd79668`, comment the derived constant blocks with their origin.
  A future reader must be able to tell which magic numbers came from where.

### 11.8 Versioning

`v0.x` until M9 completes. During v0 the API may change freely — which is the
point of getting §4 right before there are consumers.

At v1.0 the public surface is frozen under semver. Given §3.3, treat the import
path and the `Device` interface as the two things hardest to change later.

---

## 12. Glossary

The controller commands in §2.5, decoded. These are JD79668 names; other
controllers use different ones for similar operations.

| Code | Name | Meaning |
|---|---|---|
| `0x00` | PSR | Panel Setting Register — resolution flags, scan direction |
| `0x02` | POF | Power OFF |
| `0x04` | PON | Power ON |
| `0x06` | BTST_P | Booster Soft Start — charge-pump ramp settings |
| `0x07` | DSLP | Deep Sleep. Takes `0xA5` as a safety argument |
| `0x10` | DTM | Data Transmission — the framebuffer itself |
| `0x12` | DRF | Display Refresh. **This is the 20-odd seconds** |
| `0x50` | CDI | VCOM and Data Interval — border behaviour and timing |
| `0x61` | TRES | Resolution Setting — width and height, big-endian pairs |

Signal lines:

| Line | Meaning |
|---|---|
| **CS** | Chip Select. Active low. We drive it as a GPIO, not via the SPI peripheral |
| **DC** | Data/Command. Low = the byte is a command, high = the byte is data |
| **RESET** | Active low hardware reset |
| **BUSY** | Low while the panel is working, high when ready. Host pull-up |

Other terms:

- **OTP** — one-time programmable memory inside the panel. On this controller it
  holds the waveform, which is why there are no LUTs to upload.
- **LUT** — look-up table. The voltage waveform that drives the ink. On panels
  that need one uploaded, getting it wrong causes ghosting or damage.
- **Framebuffer** — here, the packed 2-bits-per-pixel byte array actually sent
  to the panel, as distinct from the `*image.Paletted` a consumer draws into.

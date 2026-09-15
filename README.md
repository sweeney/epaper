# epaper

A Go library for driving e-ink panels from a Raspberry Pi.

Make the panel disappear, so that a program that wants to put something on an
e-ink screen thinks only about the picture.

> **Status: v0.** Working end to end on a Pi 4B with an Inky wHAT 4.2" — the
> test card draws on the panel in 25.6 s. The API may still change before
> v1.0. See [`PLAN.md`](PLAN.md) for the design and the milestone list, and
> [`CONTRIBUTING.md`](CONTRIBUTING.md) for how to add a controller.

## What it looks like

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

	c := render.NewCanvasFor(dev) // bounds and palette both come from the device
	c.Fill(epaper.White)
	c.Rect(image.Rect(0, 0, 400, 30), epaper.Red)
	c.Text(image.Pt(8, 6), "HELLO", loadFont(), epaper.White)
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

Nothing in that mentions SPI, GPIO, chip-select, bit packing or refresh
sequencing. That is the bar.

## Design rules

These come from things that actually bit us on the bench, not from taste.

| Rule | Why |
|---|---|
| Never `os.Exit`, never panic on hardware state | A library that kills its caller cannot be used in a service |
| Every failure is a returned `error` | Sentinel errors, so callers can `errors.Is` and act |
| No cgo | `GOOS=linux GOARCH=arm64 go build` gives a static binary, cross-compiled from a Mac |
| No root required | A user in `spi`, `i2c` and `gpio` can drive the panel |
| `context.Context` on anything that blocks | A refresh takes ~25 s |
| Measure, don't inherit | Copied constants are cited; copied *delays* are tested for load-bearingness first |

## Supported hardware

| | |
|---|---|
| Controller | JD79668 |
| Board | Inky wHAT 4.2" (Pimoroni) |
| Resolution | 400 × 300 |
| Inks | Black, white, yellow and red — **all four simultaneously** |
| Refresh | ~25 s, full panel only |

The split is **by controller**, with a thin board package on top, because that
is the shape the hardware already has — controllers are made by Solomon Systech
and Fitipower, then rebadged by every board vendor. Adding a new controller
means a new `driver/` package and one line in `inky.Open`'s switch. See
`CONTRIBUTING.md`.

### Pi setup

`dtoverlay=spi0-0cs` is **required** in `/boot/firmware/config.txt`. Without it
the kernel SPI driver owns GPIO 8 and the panel cannot be addressed; the failure
presents as a chip-select conflict, not as a missing device.

The user must be in the `spi`, `i2c` and `gpio` groups. Root is not needed.

## Try it

```bash
go run ./cmd/epaper-testcard -png card.png    # no hardware needed
go run ./cmd/epaper-testcard                  # draw on the panel, ~25 s
```

One line proves a panel, its wiring and the whole stack:

```go
dev, err := inky.Open()
if err != nil {
	log.Fatal(err)
}
defer dev.Close()

if err := testcard.Show(ctx, dev, "bench check"); err != nil {
	log.Fatal(err)
}
```

## Examples

[`examples/`](examples) has four, smallest first:

| | | Needs a panel? |
|---|---|---|
| [`hello`](examples/hello) | The smallest useful program | Yes |
| [`offline`](examples/offline) | Building a layout with no hardware | **No** |
| [`dashboard`](examples/dashboard) | A realistic status panel | Optional |
| [`consumertest`](examples/consumertest) | Testing *your own* display code | **No** |

Start with `offline`. The fastest way to build for e-ink is to not use the
panel: a refresh takes 25 seconds, a PNG takes 25 milliseconds and you can
diff it. Then swap `mock.New` for `inky.Open` — that one line is the only
difference.

## Testing without a Pi

Most of this library is pure and runs on a laptop:

```bash
make test     # pure packages, fast
make build    # cross-compile for the Pi
make check    # everything CI runs
```

`testdata/` holds fixtures captured from real hardware — a conformance pattern
that is byte-exact against the vendor library, and seven EEPROM records
covering the real board plus six failure modes. See
[`testdata/README.md`](testdata/README.md); those fixtures are oracles, so if
your code disagrees with one, your code is wrong.

The `mock` package implements `epaper.Device` in memory, so a consumer can build
and test an entire display program with no hardware present.

Hardware tests are behind `//go:build hardware` and never run in CI:

```bash
make test-hw HOST=user@your-pi
```

### How far the fixtures go

The conformance pattern is reproduced **byte for byte** by the `render`
package: all 120,000 palette indices, and the 30,000 packed bytes the vendor
library would have sent. Every shape in it follows a rule written down in
`testdata/README.md` rather than inherited from another library's rasteriser,
which is what makes that comparison possible at all.

## Licence

MIT — see [`LICENSE`](LICENSE).

The JD79668 command sequence, its payload constants, the EEPROM layout and the
display-variant table are derived from Pimoroni's [`inky`][inky] library, which
is also MIT licensed. That derivation is acknowledged in [`NOTICE`](NOTICE), the
vendor source is reproduced under `reference/vendor-inky/` so any constant can
be checked against its origin, and each derived block in our source names the
file it came from.

[inky]: https://github.com/pimoroni/inky

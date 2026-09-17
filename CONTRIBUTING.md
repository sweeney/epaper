# Contributing

## The one rule

**Measure, don't inherit.** Where a constant comes from another project, say
so and cite the file. Where a *delay* comes from another project, test whether
it is load-bearing before copying it. Most of the surprises in this repo came
from a number nobody had checked.

## Working on it

```bash
make test     # pure packages, fast, no hardware
make check    # everything CI runs: tidy, lint, test, cross-compile
make golden   # regenerate testdata/golden — then LOOK at the diff
make report   # run the tests and build test-report.html
make fuzz     # a short run over the parsers
```

`make report` writes a self-contained HTML page: results, per-package
coverage, and the rendered goldens. CI builds the same page and attaches it to
every run as the `test-report` artifact, passing or failing.

When a golden does not match, the page shows **expected and actual side by
side**. That is the point of it — for this library most of what the tests
assert is what got drawn, and "3 failed" tells you nothing about whether the
test card still looks right.

Hardware tests live in `./hwtest` behind the `hardware` build tag and never
run in CI:

```bash
make test-hw HOST=user@your-pi
make testcard HOST=user@your-pi                      # draws on the panel, ~20 s
make testcard HOST=user@your-pi PATTERN=orientation  # which way up is it?
```

Off the panel, `make testcard-png` renders the card at **every** supported
resolution, and `make panels` lists them. Use it: the card is laid out from the
panel's size, so "it looks right" is a claim about one geometry until you have
looked at the others.

`go test -c` compiles one package, which is why every hardware test is in
`hwtest` rather than beside the code it exercises.

### Tests come first

Red-green, throughout. The failing test is written and seen to fail before the
implementation exists. This is not ceremony: on hardware with a 20-second
feedback loop, a test that has never failed is a test you cannot trust.

Three habits that have already paid for themselves:

- **When a test passes first time, break something and check it fails.** The
  driver's command-sequence test was verified this way — changing one payload
  byte from `0x37` to `0x38` must fail, and it does. The concurrency test was
  verified by removing the mutex; the packing optimisation, by the conformance
  fixture it could not have passed by accident.
- **Look at every golden you regenerate.** A golden accepted without being
  looked at asserts nothing at all; it records whatever the code does today,
  including the bug you were about to find. Reviewing them has caught an
  invisible crosshair, a legibility ladder drawn over a pixel grid, a ladder
  showing the same size twice, and text with strokes missing.
- **Judge rendering on the panel, not on a preview.** A PNG at 2× on a bright
  screen flatters exactly the defects that ruin small text on 1-bit hardware.
  Small text was declared fixed twice from a preview and was wrong both times;
  a photograph of the panel settled it in one look. A refresh costs 20 seconds,
  which is cheap next to being wrong — and several candidates drawn on **one**
  card answers the question in a single refresh.

### Fixtures are oracles

`testdata/` was captured from real hardware. If your code disagrees with a
fixture, your code is wrong — go and read `testdata/README.md` before changing
one. The exception is §6 of that file: if a fixture and the hardware ever
genuinely disagree, trust the hardware, re-capture, and write down what
changed.

## Adding a controller

The controllers are not made by the board vendors. JD79668, SSD1683, SSD1608
and UC8159 come from Fitipower, Solomon Systech and others, and every vendor
rebadges them. A controller driver will drive *any* board carrying that chip;
only the pin map and the detection mechanism differ. So the split is by
controller, with a thin board package on top.

Adding one should be a new package plus one line. Concretely:

**1. Write `driver/<controller>/`.** Model it on `driver/jd79668`. It must:

- depend on a `Conn` interface it declares itself, never on `internal/spidev`.
  That is the whole reason the command sequence can be tested with no Pi.
- declare its own `Palette`. **Slice position is the wire index** — element 0
  is the byte value 0 the controller receives. Pin the order in a test;
  reordering it for tidiness would silently swap every panel's colours.
- cite its origin. Every magic number needs a comment naming the file it came
  from, and the vendor source should be added under `reference/`.
- serialise `Show` with a mutex, and validate the image *before* anything
  reaches the transport.

**2. Write the recording test.** This is the valuable one. Assert the exact
byte sequence against a fake `Conn`, the way
`driver/jd79668.TestShowEmitsTheExactSequence` does. A silent mistake here is
a blank panel and a very bad afternoon, 20 seconds at a time; in a test it is
microseconds.

**3. Pin the frame layout against the vendor, not against your reading of it.**
If the controller does anything other than send the buffer row-major — the
JD79661 pads one axis and rotates a quarter turn — then the layout is the one
part you cannot check by eye, and a sign error there draws a complete,
correctly coloured, upside-down picture. Capture a matched pair of
indices-in/bytes-out from the real vendor library, the way
`tools/frame_jd79661.py` does, and assert byte equality. `PLAN.md` §13.2 is a
worked example.

**4. Add one case to `driverFor` in `inky.OpenWith`,** mapping the EEPROM's
display variant to your driver, **and one entry to `inky.SupportedPanels()`**.
A test checks the two against each other in both directions, and checks the
geometry against the captured EEPROMs, so they cannot drift. Everything that
has to cover "every panel" is driven from that list rather than from a second
copy — the test card goldens, `-size all`, the examples' tests — so this is
what puts your panel in CI's report. If your board is not a Pimoroni one, write
a sibling of the `inky` package instead — it is about 200 lines, most of it the
EEPROM layout.

**5. Draw the orientation card on it.** `make testcard HOST=... PATTERN=orientation`.
Four differently-inked corners and an arrow, so all eight ways of being wrong
look different. This is the only check that catches a rotation or a flip, and
it costs one refresh.

**6. If it needs more than that, abstract then — not now.** There is
deliberately no driver registry and no capability negotiation. Two controllers
in, the only thing that has been worth abstracting is the supported-panel list,
and that was because three separate places needed to iterate it.

### What the second controller actually cost

Worth reading before assuming the next one is a parameter change. The JD79661
and the JD79668 share a command vocabulary, a refresh sequence, a pin map, a
palette and a reset pulse. They differ in their init registers, in what `TRES`
is told, and in their frame layout — and the EEPROM's display-variant byte is
the *only* thing that distinguishes the boards. Everything else about them,
including the colour string and the PCB revision, is identical.

`driver/jd79661.TestInitIsNotTheJD79668Sequence` exists to stop a future
tidy-up from merging them. Do not delete it.

## Orientation

There is no rotation in this library, on purpose — `PLAN.md` §9.9. Before
adding one, read that section: the question is not whether rotation is useful
but *where the knowledge of "which way up" belongs*, and there is a real
argument for putting it in the driver, because the JD79661's controller frame
is natively portrait and the driver already rotates it to present landscape.

Meanwhile `examples/portrait` is the recipe, and its tests are the thing to
copy if you touch any rotation anywhere: pinned by corner, plus
four-quarter-turns-is-the-identity. Every wrong rotation looks entirely
plausible — a mirror, an anticlockwise turn and a 180 all produce a complete,
correctly coloured picture — so "it drew something" proves nothing. All four
variants are caught by those two tests.

And the sentence everyone gets backwards: **the content rotates clockwise, so
the panel turns anticlockwise.**

## Adding text

Use a bitmap font below about 16px. This is not a preference:

A scaled outline font cannot render small text on a display with no
intermediate tones. A stem is roughly one pixel wide and lands at an arbitrary
sub-pixel position, so after thresholding some stems come out one pixel wide
and their neighbours two. It reads as bad letter-spacing rather than as missing
ink, which is why it is easy to misdiagnose — and no threshold fixes it, since
raising it drops strokes and lowering it doubles them. Both were shipped here
before the cause was understood.

Hinting is what would solve it, and FreeType does it well, which is why the
same text looks fine from Python. `x/image`'s hinting does not at these sizes.

`render.ScaleFace` scales a bitmap face by whole multiples for large text, so a
heading is exactly as crisp as the body. `render.FontFamily` documents the
whole rule.

## Adding a drawing primitive

`render` primitives must be specified, not inherited. Write the rule down in
`testdata/README.md` in integer arithmetic, the way the ellipse, line, thick
line and polygon rules are, and implement from the specification.

Write down the consequences you are choosing, too, not just the algorithm. The
thick-line rule says outright that a diagonal reads heavier than an
axis-aligned line of the same weight, and that an even weight is biased right
and down. Both are real and neither is a bug; a reader who finds them on a
panel without finding them in the spec will reasonably file one.

This matters because the alternative was tried and rejected: the conformance
fixture originally drew its geometry with PIL's rasterisers, which are exactly
as unportable as glyph hinting. Matching them would have pinned this library's
public API to another project's internals forever. See `PLAN.md` §9.8.

## Errors

- Every failure is a returned `error`. Nothing calls `os.Exit`; nothing panics
  on hardware state. A library that kills its caller cannot be used in a
  service, which is what this one is for.
- Wrap a sentinel so callers can `errors.Is`.
- Put the fix in the message where there is one. `ErrChipSelectBusy` says *"add
  `dtoverlay=spi0-0cs` to config.txt and reboot"*, because working that out
  from "line 8 is busy" is a twenty-minute debugging session.

## Dependencies

There are three, and each needed an argument:

| Module | Why |
|---|---|
| `golang.org/x/image` | `font.Face` for text |
| `golang.org/x/sys` | ioctl |
| `github.com/warthog618/go-gpiocdev` | GPIO chardev; isolated behind `internal/gpiocdev` so it can be swapped or inlined |

No cgo, ever: `GOOS=linux GOARCH=arm64 go build` must produce a static binary
cross-compiled from a laptop, with no toolchain on the Pi. Any new dependency
needs a justification in the PR.

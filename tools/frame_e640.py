#!/usr/bin/env python3
"""Generate the E640 frame-layout fixture.

Runs ON THE PI, in the reference venv, with the Inky Impression 4.0" attached
— see testdata/README.md §5. It uses the real vendor library as the oracle, but
replaces _update so nothing touches SPI, GPIO or the panel.

What this pins is the three things about the E640 that cannot be read off the
vendor source without working through numpy:

  1. the REMAP. The image's six palette indices are not the wire values: the
     controller skips 4, so blue is sent as 5 and green as 6. The vendor does
     this in set_image(), and this script goes THROUGH set_image() so the remap
     in the fixture is the vendor's, not ours.
  2. the ROTATION. The controller's frame is portrait 400x600; the picture is
     landscape 600x400, and show() turns it a quarter turn clockwise.
  3. the PACKING. Four bits per pixel, two pixels to a byte, high nibble first.

The pattern is a pure function of (x, y) with the four corners forced to four
different inks, as in frame_jd79661.py. Every ink appears, including the two
either side of the gap in the wire values.

set_image() quantises with Pillow. That is safe here, and only here, because
the input uses exactly the six pure colours of the vendor's DESATURATED_PALETTE
and saturation=0 makes the quantisation palette those same six colours: every
pixel is an exact match, so there is no error for Floyd-Steinberg to diffuse.
The script then CHECKS that — it asserts the vendor's buffer is the input
remapped — rather than trusting it.

Outputs:
    e640-frame.idx   240000 palette indices, one per pixel, row-major, in the
                     order black, white, yellow, red, blue, green (0..5)
    e640-frame.bin   the 120000-byte packed framebuffer the vendor library
                     would send to the panel — the oracle for the Go port
    e640-frame.png   what the input looks like, for a human
"""
import sys

import numpy
from PIL import Image

W, H = 600, 400
BLACK, WHITE, YELLOW, RED, BLUE, GREEN = range(6)

# The vendor's pure colours, in palette order. Taken from the driver itself at
# run time rather than copied, so a different release cannot silently disagree.
def pure_colours(dev):
    return [tuple(c) for c in dev.DESATURATED_PALETTE[:6]]


def build():
    """A deterministic index pattern. Every pixel is a function of (x, y)."""
    idx = numpy.fromfunction(
        lambda y, x: (x * 7 + y * 3) % 6, (H, W), dtype=int
    ).astype(numpy.uint8)

    # Four distinct corners, deliberately not in palette order, and two of them
    # the inks either side of the gap, so a remap error moves a corner too.
    idx[0][0] = RED
    idx[0][W - 1] = BLUE
    idx[H - 1][0] = GREEN
    idx[H - 1][W - 1] = YELLOW
    return idx


def pack_via_vendor(idx):
    """Get the packed bytes the vendor library would actually send."""
    from inky.auto import auto

    dev = auto()
    if (dev.width, dev.height) != (W, H):
        raise SystemExit(f"attached panel is {dev.width}x{dev.height}, want {W}x{H}")

    colours = pure_colours(dev)
    rgb = numpy.array(colours, dtype=numpy.uint8)[idx]
    dev.set_image(Image.fromarray(rgb, "RGB"), saturation=0.0)

    # The check that makes going through Pillow safe: the vendor's buffer must
    # be the input with the remap applied and nothing else changed.
    want = numpy.array([0, 1, 2, 3, 5, 6], dtype=numpy.uint8)[idx]
    if not numpy.array_equal(numpy.asarray(dev.buf, dtype=numpy.uint8), want):
        bad = int(numpy.count_nonzero(numpy.asarray(dev.buf, dtype=numpy.uint8) != want))
        raise SystemExit(f"set_image changed {bad} pixels beyond the remap; the fixture would be wrong")

    captured = {}
    dev._update = lambda buf: captured.update(buf=bytes(buf))
    dev.show()
    return captured["buf"], colours


def main():
    idx = build()

    with open("e640-frame.idx", "wb") as f:
        f.write(idx.tobytes())
    print(f"wrote e640-frame.idx ({idx.size} indices)")

    try:
        buf, colours = pack_via_vendor(idx)
    except Exception as e:
        print(f"SKIPPED e640-frame.bin: {type(e).__name__}: {e}", file=sys.stderr)
        return
    with open("e640-frame.bin", "wb") as f:
        f.write(buf)
    print(f"wrote e640-frame.bin ({len(buf)} bytes packed)")

    # A preview, purely so a human can look at what the fixture contains.
    pal = []
    for rgb in colours:
        pal += list(rgb)
    pal += [0] * (768 - len(pal))
    img = Image.frombytes("P", (W, H), idx.tobytes())
    img.putpalette(pal)
    img.convert("RGB").save("e640-frame.png")
    print("wrote e640-frame.png")


if __name__ == "__main__":
    main()

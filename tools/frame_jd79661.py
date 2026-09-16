#!/usr/bin/env python3
"""Generate the JD79661 frame-layout fixture.

Runs ON THE PI, in the reference venv, with the pHAT attached — see
testdata/README.md §4. It uses the real vendor library as the oracle, but
replaces _update so nothing touches SPI, GPIO or the panel.

What this pins is narrow and deliberate: the JD79661's FRAME LAYOUT, which is
the one thing about this controller that cannot be read off the vendor source
without working through numpy. Everything else the vendor library does —
dithering, quantisation, geometry — is panel-independent and already pinned by
testdata/conformance/, which is 400x300 and needs no second copy here.

The pattern is a pure function of (x, y) with the four corners forced to four
different inks. Corners are what catch a transposition, a flip, or an
off-by-one in the six rows of padding; each of those otherwise yields an image
that looks broadly plausible.

Outputs:
    jd79661-frame.idx   30500 raw palette indices, one per pixel, row-major
    jd79661-frame.bin   the 8000-byte packed framebuffer the vendor library
                        would send to the panel — the oracle for the Go port
"""
import sys

import numpy
from PIL import Image

W, H = 250, 122
BLACK, WHITE, YELLOW, RED = 0, 1, 2, 3


def build():
    """A deterministic index pattern. Every pixel is a function of (x, y)."""
    idx = numpy.fromfunction(
        lambda y, x: (x * 7 + y * 3) % 4, (H, W), dtype=int
    ).astype(numpy.uint8)

    # Four distinct corners. Deliberately all different, and deliberately not
    # in palette order, so no symmetry of the transform can preserve them.
    idx[0][0] = RED
    idx[0][W - 1] = YELLOW
    idx[H - 1][0] = WHITE
    idx[H - 1][W - 1] = BLACK
    return idx


def pack_via_vendor(idx):
    """Get the packed bytes the vendor library would actually send.

    Uses the real inky driver so this is a true oracle for the packing, but
    replaces _update so nothing touches SPI, GPIO or the panel. set_image is
    bypassed too: quantisation is not what is under test here, and going
    through it would let PIL's dither decide what the fixture contains.
    """
    from inky.auto import auto

    dev = auto()
    if (dev.width, dev.height) != (W, H):
        raise SystemExit(f"attached panel is {dev.width}x{dev.height}, want {W}x{H}")

    captured = {}
    dev._update = lambda buf: captured.update(buf=bytes(buf))
    dev.buf = numpy.array(idx, dtype=numpy.uint8).reshape((dev.rows, dev.cols))
    dev.show()
    return captured["buf"]


def main():
    idx = build()

    with open("jd79661-frame.idx", "wb") as f:
        f.write(idx.tobytes())
    print(f"wrote jd79661-frame.idx ({idx.size} indices)")

    try:
        buf = pack_via_vendor(idx)
    except Exception as e:
        print(f"SKIPPED jd79661-frame.bin: {type(e).__name__}: {e}", file=sys.stderr)
        return
    with open("jd79661-frame.bin", "wb") as f:
        f.write(buf)
    print(f"wrote jd79661-frame.bin ({len(buf)} bytes packed)")

    # A preview, purely so a human can look at what the fixture contains.
    preview_rgb = [(0, 0, 0), (255, 255, 255), (235, 205, 40), (190, 45, 40)]
    pal = []
    for rgb in preview_rgb:
        pal += list(rgb)
    pal += [0] * (768 - len(pal))
    img = Image.frombytes("P", (W, H), idx.tobytes())
    img.putpalette(pal)
    img.convert("RGB").save("jd79661-frame.png")
    print("wrote jd79661-frame.png")


if __name__ == "__main__":
    main()

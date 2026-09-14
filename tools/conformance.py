#!/usr/bin/env python3
"""Generate the epaper conformance fixture.

A deterministic 400x300 pattern built ONLY from geometry — no fonts, no time,
no randomness, and no PIL rasterisers. Every pixel is a pure function of its
coordinates, computed here from rules written down in testdata/README.md §2.

Why no text: Pillow and Go's x/image/font rasterise glyphs differently, so a
text-bearing image can never be compared byte-for-byte across the two.

Why no ImageDraw: the same argument, one level down. PIL's ellipse, line and
polygon rasterisers are every bit as much an unportable implementation detail
as glyph hinting is, and Go has no stdlib equivalent to match. An earlier
version of this script used them, and bands 1-5 were reproducible from the
documented rules while band 6 was not. Every shape below is therefore drawn
from an integer algorithm specified in testdata/README.md, which the Go render
package implements directly rather than reverse-engineering from this output.

PIL is still used for two things it *is* authoritative about: writing the PNG,
and (via the vendor library) packing the framebuffer.

Outputs:
    conformance.png   what it should look like
    conformance.idx   120000 raw palette indices, one per pixel
    conformance.bin   the 30000-byte packed framebuffer the vendor library
                      would send to the panel — the oracle for the Go port
"""
import sys

from PIL import Image

W, H = 400, 300
BLACK, WHITE, YELLOW, RED = 0, 1, 2, 3
PREVIEW_RGB = [(0, 0, 0), (255, 255, 255), (255, 255, 0), (255, 0, 0)]

# The exact ordered-dither matrix. Go must use this, in this orientation, with
# this threshold rule:  ink if ratio > (BAYER[y%4][x%4] + 0.5) / 16
BAYER4 = [
    [0, 8, 2, 10],
    [12, 4, 14, 6],
    [3, 11, 1, 9],
    [15, 7, 13, 5],
]


# --------------------------------------------------------------------------
# Portable rasterisation. Integer arithmetic throughout: no floating point, no
# rounding mode, nothing that can differ between Python and Go.
# --------------------------------------------------------------------------

def fill_rect(buf, x0, y0, x1, y1, ink):
    """Fill the inclusive box (x0,y0)-(x1,y1)."""
    for y in range(y0, y1 + 1):
        row = y * W
        for x in range(x0, x1 + 1):
            buf[row + x] = ink


def stroke_rect(buf, x0, y0, x1, y1, ink):
    """Draw a 1px border on the inclusive box (x0,y0)-(x1,y1)."""
    for x in range(x0, x1 + 1):
        buf[y0 * W + x] = ink
        buf[y1 * W + x] = ink
    for y in range(y0, y1 + 1):
        buf[y * W + x0] = ink
        buf[y * W + x1] = ink


def ellipse_inside(x, y, x0, y0, x1, y1):
    """Is pixel (x,y) inside the ellipse inscribed in the inclusive box?

    Doubled coordinates keep a half-integer centre in the integers:

        dx = 2x - (x0+x1)      ax = x1-x0      (both twice the real value)
        dy = 2y - (y0+y1)      ay = y1-y0

        inside  <=>  dx^2*ay^2 + dy^2*ax^2 <= ax^2*ay^2

    which is the ordinary ellipse equation with every term multiplied by
    (2*ax*ay)^2. The ellipse touches each side of the box at exactly one point.
    """
    dx = 2 * x - (x0 + x1)
    dy = 2 * y - (y0 + y1)
    ax = x1 - x0
    ay = y1 - y0
    return dx * dx * ay * ay + dy * dy * ax * ax <= ax * ax * ay * ay


def fill_ellipse(buf, x0, y0, x1, y1, ink):
    """Fill the ellipse inscribed in the inclusive box."""
    for y in range(y0, y1 + 1):
        row = y * W
        for x in range(x0, x1 + 1):
            if ellipse_inside(x, y, x0, y0, x1, y1):
                buf[row + x] = ink


def stroke_ellipse(buf, x0, y0, x1, y1, ink):
    """Outline the ellipse: inside pixels having a 4-neighbour outside.

    Deriving the outline from the same inside-test as the fill is what keeps
    the two exactly consistent — there is no second algorithm to disagree.
    """
    for y in range(y0, y1 + 1):
        row = y * W
        for x in range(x0, x1 + 1):
            if not ellipse_inside(x, y, x0, y0, x1, y1):
                continue
            if all(ellipse_inside(nx, ny, x0, y0, x1, y1)
                   for nx, ny in ((x - 1, y), (x + 1, y), (x, y - 1), (x, y + 1))):
                continue
            buf[row + x] = ink


def draw_line(buf, x0, y0, x1, y1, ink):
    """Bresenham's line, the canonical all-octant integer form.

    Endpoints are both included. Note this is NOT symmetric in its arguments:
    swapping the endpoints can shift the line by a pixel, so the argument order
    below is part of the fixture.
    """
    dx = abs(x1 - x0)
    sx = 1 if x0 < x1 else -1
    dy = -abs(y1 - y0)
    sy = 1 if y0 < y1 else -1
    err = dx + dy
    while True:
        buf[y0 * W + x0] = ink
        if x0 == x1 and y0 == y1:
            return
        e2 = 2 * err
        if e2 >= dy:
            err += dy
            x0 += sx
        if e2 <= dx:
            err += dx
            y0 += sy


def fill_convex_polygon(buf, pts, ink):
    """Fill a convex polygon by integer half-space test.

    For each edge a->b the edge function

        e(p) = (b.x-a.x)*(p.y-a.y) - (b.y-a.y)*(p.x-a.x)

    is positive on one side and negative on the other. A pixel is inside when
    no edge reports it strictly outside, tested at the pixel's integer
    coordinate. Accepting e == 0 puts boundary pixels inside, so the polygon
    includes its own outline.

    Winding-agnostic: the test is "all non-negative OR all non-positive", so
    the vertex order does not have to be known in advance.
    """
    xs = [p[0] for p in pts]
    ys = [p[1] for p in pts]
    for y in range(min(ys), max(ys) + 1):
        row = y * W
        for x in range(min(xs), max(xs) + 1):
            pos = neg = False
            for i, (ax, ay) in enumerate(pts):
                bx, by = pts[(i + 1) % len(pts)]
                e = (bx - ax) * (y - ay) - (by - ay) * (x - ax)
                if e > 0:
                    pos = True
                elif e < 0:
                    neg = True
            if not (pos and neg):
                buf[row + x] = ink


# --------------------------------------------------------------------------
# The pattern
# --------------------------------------------------------------------------

def build():
    buf = bytearray([WHITE]) * (W * H)

    # --- band 1 (y 0..39): flat quadrants of all four inks
    for i, ink in enumerate((BLACK, WHITE, YELLOW, RED)):
        fill_rect(buf, i * 100, 0, i * 100 + 99, 39, ink)

    # --- band 2 (y 40..99): 1px horizontal rules at pitch 2,3,4,5
    for i, pitch in enumerate((2, 3, 4, 5)):
        x0 = i * 100
        for y in range(40, 100):
            if (y - 40) % pitch == 0:
                for x in range(x0, x0 + 100):
                    buf[y * W + x] = BLACK

    # --- band 3 (y 100..159): checkerboards, cell 1/2/4, plus a yellow/red one
    for i, (cell, a, b) in enumerate(
        ((1, BLACK, WHITE), (2, BLACK, WHITE), (4, BLACK, WHITE), (1, YELLOW, RED))
    ):
        x0 = i * 100
        for y in range(100, 160):
            for x in range(x0, x0 + 100):
                buf[y * W + x] = a if ((x // cell + y // cell) % 2 == 0) else b

    # --- band 4 (y 160..219): Bayer ramps, black-on-white, ratio 0.0..1.0
    for y in range(160, 220):
        for x in range(W):
            ratio = x / (W - 1)
            t = (BAYER4[y % 4][x % 4] + 0.5) / 16.0
            buf[y * W + x] = BLACK if ratio > t else WHITE

    # --- band 5 (y 220..259): Bayer ramps in the accent inks
    for y in range(220, 240):
        for x in range(W):
            ratio = x / (W - 1)
            t = (BAYER4[y % 4][x % 4] + 0.5) / 16.0
            buf[y * W + x] = YELLOW if ratio > t else WHITE
    for y in range(240, 260):
        for x in range(W):
            ratio = x / (W - 1)
            t = (BAYER4[y % 4][x % 4] + 0.5) / 16.0
            buf[y * W + x] = RED if ratio > t else BLACK

    # --- band 6 (y 260..299): geometry primitives, all portable (see above)
    fill_rect(buf, 4, 264, 96, 295, RED)
    stroke_rect(buf, 4, 264, 96, 295, BLACK)
    fill_ellipse(buf, 104, 264, 164, 295, YELLOW)
    stroke_ellipse(buf, 104, 264, 164, 295, BLACK)
    draw_line(buf, 172, 295, 260, 264, BLACK)
    draw_line(buf, 172, 264, 260, 295, RED)
    fill_convex_polygon(buf, [(272, 295), (316, 264), (360, 295)], BLACK)

    # single-pixel corner markers: catch off-by-one and flip errors
    for (x, y), ink in (((396, 296), RED), ((399, 299), BLACK),
                        ((396, 299), YELLOW), ((399, 296), WHITE)):
        buf[y * W + x] = ink

    img = Image.frombytes("P", (W, H), bytes(buf))
    return img


def pack_via_vendor(img):
    """Get the packed bytes the vendor library would actually send.

    Uses the real inky driver so this is a true oracle for the packing, but
    replaces _update so nothing touches SPI, GPIO or the panel.
    """
    from inky.auto import auto

    dev = auto()
    captured = {}
    dev._update = lambda buf: captured.update(buf=bytes(buf))
    dev.set_image(img)
    dev.show()
    return captured["buf"]


def main():
    img = build()

    pal = []
    for rgb in PREVIEW_RGB:
        pal += list(rgb)
    pal += [0] * (768 - len(pal))
    preview = img.copy()
    preview.putpalette(pal)
    preview.convert("RGB").save("conformance.png")
    print("wrote conformance.png")

    # Raw palette indices, one byte per pixel — lets Go check its *image*
    # before worrying about packing.
    with open("conformance.idx", "wb") as f:
        f.write(bytes(img.tobytes()))
    print(f"wrote conformance.idx ({W * H} bytes, one index per pixel)")

    try:
        buf = pack_via_vendor(img)
    except Exception as e:
        print(f"SKIPPED conformance.bin: {type(e).__name__}: {e}", file=sys.stderr)
        return
    with open("conformance.bin", "wb") as f:
        f.write(buf)
    print(f"wrote conformance.bin ({len(buf)} bytes packed)")


if __name__ == "__main__":
    main()

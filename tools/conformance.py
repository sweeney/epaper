#!/usr/bin/env python3
"""Generate the epaper conformance fixture.

A deterministic 400x300 pattern built ONLY from geometry — no fonts, no time,
no randomness. Every pixel is a pure function of its coordinates.

Why no text: Pillow and Go's x/image/font rasterise glyphs differently, so a
text-bearing image can never be compared byte-for-byte across the two. Keeping
this pattern font-free is what makes an exact comparison possible at all.

Outputs:
    conformance.png   what it should look like
    conformance.bin   the 30000-byte packed framebuffer the vendor library
                      would send to the panel — the oracle for the Go port
"""
import sys

from PIL import Image, ImageDraw

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


def build():
    img = Image.new("P", (W, H), WHITE)
    d = ImageDraw.Draw(img)
    px = img.load()

    # --- band 1 (y 0..39): flat quadrants of all four inks
    for i, ink in enumerate((BLACK, WHITE, YELLOW, RED)):
        d.rectangle([i * 100, 0, i * 100 + 99, 39], fill=ink)

    # --- band 2 (y 40..99): 1px horizontal rules at pitch 2,3,4,5
    for i, pitch in enumerate((2, 3, 4, 5)):
        x0 = i * 100
        for y in range(40, 100):
            if (y - 40) % pitch == 0:
                for x in range(x0, x0 + 100):
                    px[x, y] = BLACK

    # --- band 3 (y 100..159): checkerboards, cell 1/2/4, plus a yellow/red one
    for i, (cell, a, b) in enumerate(
        ((1, BLACK, WHITE), (2, BLACK, WHITE), (4, BLACK, WHITE), (1, YELLOW, RED))
    ):
        x0 = i * 100
        for y in range(100, 160):
            for x in range(x0, x0 + 100):
                px[x, y] = a if ((x // cell + y // cell) % 2 == 0) else b

    # --- band 4 (y 160..219): Bayer ramps, black-on-white, ratio 0.0..1.0
    for y in range(160, 220):
        for x in range(W):
            ratio = x / (W - 1)
            t = (BAYER4[y % 4][x % 4] + 0.5) / 16.0
            px[x, y] = BLACK if ratio > t else WHITE

    # --- band 5 (y 220..259): Bayer ramps in the accent inks
    for y in range(220, 240):
        for x in range(W):
            ratio = x / (W - 1)
            t = (BAYER4[y % 4][x % 4] + 0.5) / 16.0
            px[x, y] = YELLOW if ratio > t else WHITE
    for y in range(240, 260):
        for x in range(W):
            ratio = x / (W - 1)
            t = (BAYER4[y % 4][x % 4] + 0.5) / 16.0
            px[x, y] = RED if ratio > t else BLACK

    # --- band 6 (y 260..299): geometry primitives
    d.rectangle([4, 264, 96, 295], fill=RED, outline=BLACK)
    d.ellipse([104, 264, 164, 295], fill=YELLOW, outline=BLACK)
    d.line([(172, 295), (260, 264)], fill=BLACK, width=1)
    d.line([(172, 264), (260, 295)], fill=RED, width=1)
    d.polygon([(272, 295), (316, 264), (360, 295)], fill=BLACK)
    # single-pixel corner markers: catch off-by-one and flip errors
    for (x, y), ink in (((396, 296), RED), ((399, 299), BLACK),
                        ((396, 299), YELLOW), ((399, 296), WHITE)):
        px[x, y] = ink
    return img


def pack_via_vendor(img):
    """Get the packed bytes the vendor library would actually send.

    Uses the real inky driver so this is a true oracle, but replaces _update so
    nothing touches SPI, GPIO or the panel.
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

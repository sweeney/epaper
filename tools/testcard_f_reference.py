#!/usr/bin/env python3
"""Test Card F, for the Inky wHAT 4.2" — four inks, no greys.

    ./testcard_f.py                 # draw to the panel
    ./testcard_f.py --png out.png   # render to a file, no hardware needed

The panel has exactly four inks: BLACK, WHITE, YELLOW, RED. Test Card F needs
greys, cyan, green, magenta and blue, none of which exist here. Everything that
is not one of the four inks is therefore built out of ordered dither and
checkerboards — which the bench test proved this panel renders cleanly at 1px:

    grey            4x4 Bayer black-on-white at a given ratio -> a real wedge
    orange          1px yellow/red checkerboard
    pink            red dithered into white
    dark red        red dithered into black
    olive           yellow dithered into black

The colour bars keep the original's luminance ORDER (light to dark) rather than
pretending to hues we cannot make. That is the honest translation: Test Card F
was designed to check luminance steps and frequency response, and both of those
survive the palette loss intact.
"""
import argparse
import sys
import time

from PIL import Image, ImageDraw, ImageFont

W, H = 400, 300
BLACK, WHITE, YELLOW, RED = 0, 1, 2, 3
PREVIEW_RGB = [(0, 0, 0), (255, 255, 255), (235, 205, 40), (190, 45, 40)]

BAYER4 = [[0, 8, 2, 10], [12, 4, 14, 6], [3, 11, 1, 9], [15, 7, 13, 5]]

B = 12                      # castellation border thickness
CX, CY, CR = 200, 148, 78   # central circle (~52% of height, as the original)


def font(size, bold=False):
    paths = []
    try:
        from font_source_sans_pro import SourceSansProBold, SourceSansProRegular
        paths.append(SourceSansProBold if bold else SourceSansProRegular)
    except ImportError:
        pass
    paths.append("/System/Library/Fonts/Supplemental/Arial Bold.ttf" if bold
                 else "/System/Library/Fonts/Supplemental/Arial.ttf")
    for p in paths:
        try:
            return ImageFont.truetype(p, size)
        except Exception:
            continue
    try:
        return ImageFont.load_default(size=size)
    except TypeError:
        return ImageFont.load_default()


# --------------------------------------------------------------- ink mixing
def dither_on(px, w, h, box, ink, bg, ratio):
    """Same as dither() but for an arbitrary-sized target (the circle tile)."""
    x0, y0, x1, y1 = box
    for y in range(max(0, y0), min(h, y1)):
        for x in range(max(0, x0), min(w, x1)):
            t = (BAYER4[y % 4][x % 4] + 0.5) / 16.0
            px[x, y] = ink if ratio > t else bg


def dither(px, box, ink, bg, ratio, clip=None):
    """Ordered-dither `ink` into `bg` at `ratio` (0..1) over `box`."""
    x0, y0, x1, y1 = box
    for y in range(max(0, y0), min(H, y1)):
        for x in range(max(0, x0), min(W, x1)):
            if clip and not clip(x, y):
                continue
            t = (BAYER4[y % 4][x % 4] + 0.5) / 16.0
            px[x, y] = ink if ratio > t else bg


def checker(px, box, a, b, cell=1, clip=None):
    """Checkerboard of two inks — how we fake a hue rather than a tone."""
    x0, y0, x1, y1 = box
    for y in range(max(0, y0), min(H, y1)):
        for x in range(max(0, x0), min(W, x1)):
            if clip and not clip(x, y):
                continue
            px[x, y] = a if ((x // cell + y // cell) % 2 == 0) else b


def hatch(px, box, pitch, ink=BLACK, bg=WHITE, diag=1):
    """Diagonal frequency grating — the original's resolution wedges."""
    x0, y0, x1, y1 = box
    for y in range(y0, y1):
        for x in range(x0, x1):
            px[x, y] = ink if ((x + diag * y) % pitch) == 0 else bg


def bars(px, box, pitch, ink=BLACK, bg=WHITE):
    """Vertical bar grating."""
    x0, y0, x1, y1 = box
    for y in range(y0, y1):
        for x in range(x0, x1):
            px[x, y] = ink if ((x - x0) % pitch) < (pitch // 2) else bg


def build():
    img = Image.new("P", (W, H), WHITE)
    d = ImageDraw.Draw(img)
    px = img.load()

    # ---- main field: 50% Bayer = the "grey" Test Card F sits on
    dither(px, (0, 0, W, H), BLACK, WHITE, 0.5)

    # ---- castellated border: alternating black/white blocks, all four edges
    step = 25
    for i, x in enumerate(range(0, W, step)):
        c = BLACK if i % 2 == 0 else WHITE
        d.rectangle([x, 0, x + step - 1, B - 1], fill=c)
        d.rectangle([x, H - B, x + step - 1, H - 1], fill=c)
    for i, y in enumerate(range(0, H, step)):
        c = BLACK if i % 2 == 0 else WHITE
        d.rectangle([0, y, B - 1, y + step - 1], fill=c)
        d.rectangle([W - B, y, W - 1, y + step - 1], fill=c)

    # ---- colour bars, left and right. Original order is light->dark by
    # luminance; we keep that order using the inks and mixes we actually have.
    LADDER = [
        ("white",  lambda bx: d.rectangle(bx, fill=WHITE)),
        ("yellow", lambda bx: d.rectangle(bx, fill=YELLOW)),
        ("orange", lambda bx: checker(px, bx, YELLOW, RED)),
        ("red",    lambda bx: d.rectangle(bx, fill=RED)),
        ("olive",  lambda bx: dither(px, bx, YELLOW, BLACK, 0.5)),
        ("dkred",  lambda bx: dither(px, bx, RED, BLACK, 0.5)),
        ("black",  lambda bx: d.rectangle(bx, fill=BLACK)),
    ]
    bar_top, bar_bot = 66, 234
    seg = (bar_bot - bar_top) // len(LADDER)
    for i, (_, paint) in enumerate(LADDER):
        y0 = bar_top + i * seg
        y1 = y0 + seg
        paint((B, y0, B + 22, y1 - 1))
        paint((W - B - 23, y0, W - B - 1, y1 - 1))
    for x0 in (B, W - B - 23):
        d.rectangle([x0, bar_top, x0 + 22, bar_bot - 1], outline=BLACK)

    # ---- frequency gratings at the four inner corners
    hatch(px, (40, 20, 92, 58), 3)
    hatch(px, (308, 20, 360, 58), 3, diag=-1)
    bars(px, (40, 230, 92, 268), 4)
    bars(px, (308, 230, 360, 268), 6)
    for bx in ((40, 20, 92, 58), (308, 20, 360, 58),
               (40, 230, 92, 268), (308, 230, 360, 268)):
        d.rectangle([bx[0], bx[1], bx[2] - 1, bx[3] - 1], outline=BLACK)

    # ---- reference white/black patch, top centre
    d.rectangle([150, 22, 250, 52], fill=WHITE, outline=BLACK)
    d.rectangle([160, 30, 240, 44], fill=BLACK)

    # ---- greyscale step wedge: the thing dithering exists to prove
    wx0, wy0, wx1, wy1 = 74, 94, 104, 202
    steps = 6
    sh = (wy1 - wy0) // steps
    # White plaques: at 50% the field is itself a checkerboard, so a wedge laid
    # straight onto it loses its middle steps entirely.
    d.rectangle([wx0 - 4, wy0 - 4, wx1 + 3, wy0 + steps * sh + 3], fill=WHITE, outline=BLACK)
    d.rectangle([W - 104 - 4, wy0 - 4, W - 74 + 3, wy0 + steps * sh + 3], fill=WHITE, outline=BLACK)
    for i in range(steps):
        r = i / (steps - 1)
        dither(px, (wx0, wy0 + i * sh, wx1, wy0 + (i + 1) * sh), BLACK, WHITE, r)
    d.rectangle([wx0, wy0, wx1 - 1, wy0 + steps * sh - 1], outline=BLACK)

    # mirrored yellow->red tonal wedge on the right, using the accent inks
    ex0, ex1 = W - 104, W - 74
    MIX = [(YELLOW, WHITE, 0.5), (YELLOW, WHITE, 1.0), (YELLOW, RED, None),
           (RED, WHITE, 1.0), (RED, BLACK, 0.5), (BLACK, WHITE, 1.0)]
    for i, (a, b, r) in enumerate(MIX):
        bx = (ex0, wy0 + i * sh, ex1, wy0 + (i + 1) * sh)
        if r is None:
            checker(px, bx, a, b)
        else:
            dither(px, bx, a, b, r)
    d.rectangle([ex0, wy0, ex1 - 1, wy0 + steps * sh - 1], outline=BLACK)

    # ---- crosshair ticks
    for x in range(B, W - B, 8):
        px[x, CY] = BLACK
    for y in range(B, H - B, 8):
        px[CX, y] = BLACK

    # ---- the circle
    # Draw the picture into its own tile and composite through an ellipse
    # mask. Cleaner than clipping each shape, and nothing can spill.
    D = CR * 2
    tile = Image.new("P", (D, D), WHITE)
    td = ImageDraw.Draw(tile)
    tp = tile.load()
    # light dithered ground so the subjects read as foreground
    dither_on(tp, D, D, (0, 0, D, D), BLACK, WHITE, 0.15)

    mx, my = CR, CR          # tile-local centre

    # --- the girl, centre-left
    gxc = mx - 34
    td.polygon([(gxc - 30, D), (gxc - 18, my + 6), (gxc + 18, my + 6), (gxc + 30, D)], fill=RED)
    td.ellipse([gxc - 20, my - 40, gxc + 20, my + 2], fill=RED)          # hair mass
    td.ellipse([gxc - 13, my - 33, gxc + 13, my - 1], fill=WHITE, outline=BLACK)  # face
    td.ellipse([gxc - 8, my - 25, gxc - 5, my - 22], fill=BLACK)
    td.ellipse([gxc + 5, my - 25, gxc + 8, my - 22], fill=BLACK)
    td.arc([gxc - 7, my - 20, gxc + 7, my - 10], 20, 160, fill=BLACK)
    td.line([(gxc - 18, my + 10), (gxc + 6, my + 22)], fill=RED, width=5)  # arm to board

    # --- the blackboard with noughts and crosses
    bx0, by0 = mx + 2, my - 34
    cell = 12
    bw = 3 * cell + 12
    bh = 3 * cell + 12
    td.rectangle([bx0, by0, bx0 + bw, by0 + bh], fill=BLACK, outline=WHITE)
    gx, gy = bx0 + 6, by0 + 6
    for k in (1, 2):
        td.line([(gx + k * cell, gy), (gx + k * cell, gy + 3 * cell)], fill=WHITE)
        td.line([(gx, gy + k * cell), (gx + 3 * cell, gy + k * cell)], fill=WHITE)
    def nought(cx, cy):
        td.ellipse([cx - 4, cy - 4, cx + 4, cy + 4], outline=YELLOW)
    def cross(cx, cy):
        td.line([(cx - 4, cy - 4), (cx + 4, cy + 4)], fill=WHITE)
        td.line([(cx - 4, cy + 4), (cx + 4, cy - 4)], fill=WHITE)
    for (c, r), fn in {(0, 0): cross, (1, 1): cross, (2, 2): cross,
                       (2, 0): nought, (0, 2): nought}.items():
        fn(gx + c * cell + cell // 2, gy + r * cell + cell // 2)

    # --- Bubbles the clown, lower right
    cxc, cyc = mx + 26, my + 30
    td.ellipse([cxc - 18, cyc - 18, cxc + 18, cyc + 18], fill=WHITE, outline=BLACK)
    td.pieslice([cxc - 20, cyc - 26, cxc + 20, cyc + 4], 180, 360, fill=RED)
    td.ellipse([cxc - 9, cyc - 4, cxc - 5, cyc], fill=BLACK)
    td.ellipse([cxc + 5, cyc - 4, cxc + 9, cyc], fill=BLACK)
    td.ellipse([cxc - 4, cyc + 1, cxc + 4, cyc + 9], fill=RED)
    td.arc([cxc - 11, cyc + 4, cxc + 11, cyc + 15], 20, 160, fill=BLACK)

    # --- the balloon (green in the original; yellow is the nearest ink we own)
    td.ellipse([mx - 8, my + 34, mx + 16, my + 58], fill=YELLOW, outline=BLACK)
    td.line([(mx + 4, my + 34), (mx - 2, my + 22)], fill=BLACK)

    mask = Image.new("1", (D, D), 0)
    ImageDraw.Draw(mask).ellipse([2, 2, D - 3, D - 3], fill=1)
    img.paste(tile, (CX - CR, CY - CR), mask)
    d.ellipse([CX - CR, CY - CR, CX + CR, CY + CR], outline=BLACK)
    d.ellipse([CX - CR + 1, CY - CR + 1, CX + CR - 1, CY + CR - 1], outline=BLACK)

    # ---- identification row: BBC | F | 1, as the original
    def plaque(box, text, size, fg, bg, outline=BLACK):
        d.rectangle(box, fill=bg, outline=outline)
        fo = font(size, True)
        tw = d.textlength(text, font=fo)
        cx = (box[0] + box[2]) / 2
        cy = (box[1] + box[3]) / 2
        d.text((cx - tw / 2, cy - size * 0.68), text, font=fo, fill=fg)

    plaque((106, 234, 168, 262), "BBC", 16, WHITE, BLACK, outline=WHITE)
    plaque((178, 230, 222, 266), "F", 26, BLACK, WHITE)
    plaque((232, 234, 294, 262), "1", 20, BLACK, WHITE)

    # our own identification. On a plaque: black text on the 50% dithered
    # field is unreadable, which is exactly the lesson the grey wedge teaches.
    d.rectangle([B + 2, 270, 150, 286], fill=WHITE, outline=BLACK)
    t = font(10, True)
    d.text((B + 7, 272), "INKY wHAT 400x300", font=t, fill=BLACK)
    d.rectangle([250, 270, W - B - 2, 286], fill=WHITE, outline=BLACK)
    d.text((256, 272), "4 INKS + DITHER", font=t, fill=BLACK)
    return img


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--png", metavar="PATH")
    args = ap.parse_args()
    img = build()

    if args.png:
        pal = []
        for rgb in PREVIEW_RGB:
            pal += list(rgb)
        pal += [0] * (768 - len(pal))
        img.putpalette(pal)
        img.convert("RGB").save(args.png)
        print(f"  wrote {args.png} ({W}x{H}) — no hardware touched")
        return

    from inky.auto import auto
    inky = auto()
    inky.set_image(img)
    print("  refreshing...")
    t0 = time.monotonic()
    inky.show()
    print(f"  REFRESH TOOK: {time.monotonic() - t0:.1f}s")


if __name__ == "__main__":
    try:
        main()
    except Exception as e:
        print(f"  FAILED: {type(e).__name__}: {e}", file=sys.stderr)
        sys.exit(1)

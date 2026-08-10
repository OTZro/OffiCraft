#!/usr/bin/env python3
"""e2e_test/seven_gate/make_answer_png.py — draw a number into a PNG, and put it
NOWHERE ELSE.

This exists for one cell of the gate: 「看得到圖」. An agent that can only read
text must FAIL it, so the answer has to live in PIXELS and in nothing else —
not the message body, not the filename, not the mime, not a task, not a plan,
not any file the agent can open. That is the entire design, and it is also the
only way this cell can go quietly useless: if the answer leaks into any string
the agent can read, a text-only agent passes and THE PASS LOOKS IDENTICAL TO
THE REAL ONE. run.sh therefore scans the whole planted scene for the answer and
refuses to run if it finds it anywhere but the blob (with a positive control, so
"zero hits" can never mean "the scanner is broken").

Stdlib only — zlib and struct. No Pillow, no fonts on disk, no system deps: this
has to work on a bare CI box, and a missing dependency here would degrade into
"the cell did not run" rather than a loud failure.

  python3 make_answer_png.py 481902 /path/out.png

The digits are drawn from a 5x7 bitmap font defined below and scaled up, black
on white, with generous padding — big and unambiguous, because the question
being asked is "did the model SEE the picture", not "can it read tiny type".
"""
import struct
import sys
import zlib

# 5x7, one string per row, '#' = ink. Deliberately blocky and well separated:
# a shape a vision model can read without effort is the point — an ambiguous
# glyph would turn an agent that CAN see into a red, which is the one failure
# this cell must never manufacture.
FONT = {
    "0": ["01110", "10001", "10011", "10101", "11001", "10001", "01110"],
    "1": ["00100", "01100", "00100", "00100", "00100", "00100", "01110"],
    "2": ["01110", "10001", "00001", "00010", "00100", "01000", "11111"],
    "3": ["11111", "00010", "00100", "00010", "00001", "10001", "01110"],
    "4": ["00010", "00110", "01010", "10010", "11111", "00010", "00010"],
    "5": ["11111", "10000", "11110", "00001", "00001", "10001", "01110"],
    "6": ["00110", "01000", "10000", "11110", "10001", "10001", "01110"],
    "7": ["11111", "00001", "00010", "00100", "01000", "01000", "01000"],
    "8": ["01110", "10001", "10001", "01110", "10001", "10001", "01110"],
    "9": ["01110", "10001", "10001", "01111", "00001", "00010", "01100"],
}

GLYPH_W, GLYPH_H = 5, 7


def render(text, scale=16, pad=40, gap=2):
    """-> (width, height, rows of RGB bytes). Pure."""
    for ch in text:
        if ch not in FONT:
            raise SystemExit("make_answer_png: only digits are drawable, got %r" % ch)
    cells_w = len(text) * GLYPH_W + (len(text) - 1) * gap
    w = cells_w * scale + pad * 2
    h = GLYPH_H * scale + pad * 2
    # start all-white
    grid = [[0 for _ in range(cells_w)] for _ in range(GLYPH_H)]
    x0 = 0
    for ch in text:
        glyph = FONT[ch]
        for y in range(GLYPH_H):
            for x in range(GLYPH_W):
                if glyph[y][x] in "1#":
                    grid[y][x0 + x] = 1
        x0 += GLYPH_W + gap
    rows = []
    for py in range(h):
        row = bytearray()
        gy = (py - pad) // scale
        for px in range(w):
            gx = (px - pad) // scale
            ink = (0 <= gy < GLYPH_H and 0 <= gx < cells_w and grid[gy][gx])
            v = 0 if ink else 255
            row += bytes((v, v, v))
        rows.append(bytes(row))
    return w, h, rows


def write_png(path, w, h, rows):
    raw = b"".join(b"\x00" + r for r in rows)   # filter byte 0 per scanline

    def chunk(tag, data):
        return (struct.pack(">I", len(data)) + tag + data
                + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF))

    png = (b"\x89PNG\r\n\x1a\n"
           + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0))
           + chunk(b"IDAT", zlib.compress(raw, 9))
           + chunk(b"IEND", b""))
    # No tEXt/iTXt chunk, ever: PNG metadata is TEXT, and an answer written
    # there would be readable with `strings` — i.e. by an agent that cannot see.
    with open(path, "wb") as fh:
        fh.write(png)


def main(argv):
    if len(argv) != 3:
        print("usage: make_answer_png.py <digits> <out.png>", file=sys.stderr)
        return 2
    answer, out = argv[1], argv[2]
    w, h, rows = render(answer)
    write_png(out, w, h, rows)
    # The answer is NOT echoed on stdout: this script's output is captured in
    # logs, and a log is a text file the next reader greps.
    print("wrote %s (%dx%d)" % (out, w, h))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))

#!/usr/bin/env bash
# Generate twelve distinct PNGs with Python's stdlib (zlib-compressed PNG),
# each a solid color band so a vision model has real content to caption.
set -u
d="$1"
python3 - "$d" <<'PY'
import zlib, struct, sys, pathlib
d = pathlib.Path(sys.argv[1])
def chunk(tag, data):
    return struct.pack(">I", len(data)) + tag + data + struct.pack(">I", zlib.crc32(tag + data) & 0xffffffff)
def png(path, w, h, rgb):
    def row(y):
        return b"\x00" + bytes(rgb) * w
    raw = b"".join(row(y) for y in range(h))
    sig = b"\x89PNG\r\n\x1a\n"
    ihdr = struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0)
    path.write_bytes(sig + chunk(b"IHDR", ihdr) + chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b""))
palette = [
    (220, 40, 40), (40, 200, 60), (40, 90, 220), (240, 220, 60),
    (200, 60, 200), (255, 140, 0), (0, 200, 200), (140, 90, 50),
    (255, 182, 193), (100, 100, 100), (30, 30, 30), (250, 250, 240),
]
for i, rgb in enumerate(palette, 1):
    png(d / f"img-{i:02d}.png", 64, 64, rgb)
PY
echo "fixture image-caption-suite"

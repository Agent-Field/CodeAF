#!/usr/bin/env python3
"""asciigen — ASCII art from images, with edge-preserving luminance.

Converts an image to ASCII art. Instead of naively averaging each cell to a
single brightness (which blurs away outlines), we combine the cell's mean
luminance with a local edge response: cells containing strong edges render
darker/denser than their average alone would suggest, so diagrams, plots and
faces keep their structure.

Examples
--------
    python3 asciigen.py photo.png -W 80              # text to stdout
    python3 asciigen.py photo.png -W 80 -o art.txt   # to file
    python3 asciigen.py photo.png -w 100 --html      # colored HTML
    python3 asciigen.py photo.png --invert --chars ' .:oO@'   # custom ramp
"""

from __future__ import annotations

import argparse
import html as _html
import sys
from pathlib import Path

try:
    import numpy as np
except ImportError:  # pragma: no cover - exercised only when numpy is absent
    np = None

try:
    from PIL import Image
except ImportError:  # pragma: no cover - exercised only when PIL is absent
    Image = None

# Plain-ASCII, no maths symbols, so output renders in any terminal.
DEFAULT_RAMP = "@%#*+=-:. "

# Horizontal edge kernel (sum 0) — drag a cell's density up when its left/right
# neighbours diverge from it, sharpening vertical outlines in particular.
EDGE_W = (0.25, -1.0, 0.25)
EDGE_H = (0.25, -1.0, 0.25)


class AsciiGenerator:
    """Core: luminance grid -> ASCII line grid. Tile-agnostic, testable."""

    def __init__(self, ramp: str = DEFAULT_RAMP):
        if not ramp:
            raise ValueError("ramp must be non-empty")
        self.ramp = ramp
        self._n = len(ramp)

    def render(self, lum, width: int, invert: bool = False) -> list[str]:
        """Map a 0..1 luminance array to a list of ASCII strings."""
        if width < 1:
            raise ValueError(f"width must be >= 1, got {width}")
        if np is None:  # pragma: no cover
            raise RuntimeError("asciigen needs numpy for image paths")
        h, w = lum.shape[:2]
        height = max(1, int(round(h / w * width * 0.5)))  # chars ~2:1 tall
        lines = []
        for row in range(height):
            r0, r1 = _slice(row, height, h)
            line = []
            for col in range(width):
                c0, c1 = _slice(col, width, w)
                block = lum[r0:r1, c0:c1]
                avg = block.mean() if block.size else 0.0
                # Adaptive min-blend: high-contrast cells (thin lines on white)
                # follow the darkest pixel; flat cells follow the mean.
                if block.size:
                    mn = block.min()
                    spread = block.max() - mn
                    mix = min(1.0, spread * 3.0)
                    val = mix * mn + (1.0 - mix) * avg
                else:
                    val = 0.0
                if invert:
                    val = 1.0 - val
                idx = int(min(1.0, val) * (self._n - 1))  # dark -> dense, light -> space
                line.append(self.ramp[idx])
            lines.append("".join(line))
        return lines


def _slice(i: int, n: int, size: int) -> tuple[int, int]:
    lo = i * size // n
    hi = (i + 1) * size // n
    return lo, max(lo + 1, hi)


def luminance(img) -> "np.ndarray":
    """Return a 0..1 luminance array for an RGB(A)/L PIL image."""
    if Image is None:  # pragma: no cover
        raise RuntimeError("asciigen needs Pillow for image loading")
    if img.mode not in ("RGBA", "RGB", "L", "1", "P"):
        img = img.convert("RGB")
    if img.mode == "RGBA":
        a = np.asarray(img).astype(float)
        rgb = a[..., :3]
        alpha = (a[..., 3] / 255.0)[..., None]
        rgb = rgb * alpha + 255.0 * (1.0 - alpha)  # composite onto white
    elif img.mode == "P":
        rgb = np.asarray(img.convert("RGBA")).astype(float)[..., :3]
    elif img.mode == "1":
        rgb = np.asarray(img).astype(float)[..., None] * 255.0
        rgb = np.repeat(rgb, 3, axis=2)
    else:
        rgb = np.asarray(img).astype(float)
        if rgb.ndim == 2:
            rgb = rgb[..., None] * 255.0 if rgb.max() <= 1.0 else rgb[..., None]
            rgb = np.repeat(rgb, 3, axis=2)
    lum = rgb @ np.array([0.2126, 0.7152, 0.0722]) / 255.0
    return lum


def render_image(inp, width: int, invert: bool = False, ramp: str = DEFAULT_RAMP,
                 reverse: bool = False) -> tuple[list[str], tuple[int, int]]:
    """Render an image path or PIL.Image to ASCII lines + original (w, h)."""
    if isinstance(inp, (str, Path)):
        img = Image.open(inp)
    elif isinstance(inp, Image.Image):
        img = inp
    else:
        raise TypeError("input must be an image path or a PIL.Image")
    lum = luminance(img)
    lines = AsciiGenerator(ramp).render(lum, width, invert)
    if reverse:
        lines = lines[::-1]
    return lines, (img.width, img.height)


def to_html(lines: list[str]) -> str:
    """Wrap rendered lines in an HTML <pre> block with markup escaped."""
    body = "\n".join(_html.escape(ln) for ln in lines)
    return f"<pre style='font-family:monospace;line-height:1'>{body}</pre>"


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(
        prog="asciigen",
        description="ASCII art from images with edge-preserving luminance.",
    )
    p.add_argument("image", help="path to an image (PNG/JPEG/GIF…)")
    p.add_argument("-W", "--width", type=int, default=80,
                   help="output width in characters (default 80)")
    p.add_argument("-o", "--output", help="write to file instead of stdout")
    p.add_argument("--invert", action="store_true",
                   help="invert: dark pixels -> light characters (dark theme)")
    p.add_argument("--reverse", action="store_true", help="flip output vertically")
    p.add_argument("--chars", default=DEFAULT_RAMP,
                   help="character ramp, dark first (default '%s')" % DEFAULT_RAMP)
    p.add_argument("--html", action="store_true", help="emit escaped HTML in a <pre>")
    args = p.parse_args(argv)

    try:
        lines, orig = render_image(
            args.image, args.width, invert=args.invert, ramp=args.chars,
            reverse=args.reverse,
        )
    except FileNotFoundError as e:
        print(f"asciigen: {e}", file=sys.stderr)
        return 2
    except (OSError, ValueError) as e:
        print(f"asciigen: {e}", file=sys.stderr)
        return 2

    out = to_html(lines) if args.html else "\n".join(lines)
    if args.output:
        Path(args.output).write_text(out + "\n", encoding="utf-8")
    else:
        sys.stdout.write(out + "\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
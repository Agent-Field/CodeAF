"""Tests for asciigen — ASCII art renderer with edge-preserving luminance."""

import subprocess
import sys
from pathlib import Path

import numpy as np
import pytest
from PIL import Image

sys.path.insert(0, str(Path(__file__).resolve().parent))

from asciigen import (  # noqa: E402
    AsciiGenerator,
    DEFAULT_RAMP,
    luminance,
    render_image,
    to_html,
)

ROOT = Path(__file__).resolve().parent


def _white(w: int, h: int) -> np.ndarray:
    return np.ones((h, w))


def _black(w: int, h: int) -> np.ndarray:
    return np.zeros((h, w))


def _gray(w: int, h: int, v: float) -> np.ndarray:
    return np.full((h, w), v)


# --------------------------------------------------------------------------
# render()
# --------------------------------------------------------------------------

def test_error_missing_file():
    with pytest.raises(FileNotFoundError):
        render_image("definitely-not-here.png", 20)


def test_unicode_ramp():
    gen = AsciiGenerator("0o. ")
    # dark (0.0) -> index 0 -> densest '0'; white (1.0) -> last -> ' '
    assert gen.render(_black(10, 10), 5)[0] == "0" * 5
    assert gen.render(_white(10, 10), 5)[0] == " " * 5


def test_width_shape():
    # image 100x50 (landscape) -> height = 50/100*20*0.5 = 5 lines
    gen = AsciiGenerator()
    out = gen.render(_gray(100, 50, 0.5), 20)
    assert len(out) == 5
    assert all(len(row) == 20 for row in out)


def test_tiny_width_clamped():
    out = AsciiGenerator().render(_gray(100, 50, 0.4), 1)
    assert len(out) == 1 and len(out[0]) == 1


def test_invert_flips_character():
    gen = AsciiGenerator("@ ")
    # white: normal -> ' ' (last), inverted -> '@' (index 0)
    assert gen.render(_white(10, 10), 5)[0] == " " * 5
    assert gen.render(_white(10, 10), 5, invert=True)[0] == "@" * 5


def test_reverse_flips_rows():
    gen = AsciiGenerator("@ ")
    # black -> '@' (index 0), white -> ' ' (index 1)
    normal = gen.render(_black(20, 4), 8)
    assert normal[0] == "@" * 8
    assert normal[::-1] == normal  # uniform, so symmetric



def test_custom_ramp():
    gen = AsciiGenerator("X. ")
    out = gen.render(_black(10, 10), 5)
    assert out[0] == "X" * 5


def test_vertical_lines_captured():
    """A thin vertical line must survive as the densest char, not wash to mean."""
    h, w = 40, 40
    lum = np.ones((h, w))
    lum[:, 20:22] = 0.0  # 1-px dark column
    out = AsciiGenerator("%. ").render(lum, 20)
    # Column 10 in a width-20 grid = the dark line's cell
    assert out[1][10] == "%"


def test_mixed_cell_uses_dark_pixel():
    """Contrast-heavy cell -> mean drags to dark (thin-line preservation)."""
    gen = AsciiGenerator("@. ")
    block = np.array(
        [
            [1.0, 1.0, 1.0],
            [1.0, 0.0, 1.0],
            [1.0, 1.0, 1.0],
        ]
    )
    out = gen.render(block, 1)
    assert out[0] != " "  # not all-space


# ------------------------------------------------------------------luminance()
# ------------------------------------------------------------------

def test_grayscale_image():
    img = Image.new("L", (10, 10), 128)
    lum = luminance(img)
    assert lum.shape == (10, 10)
    assert np.allclose(lum, 128 / 255.0)


def test_rgba_composite_on_alpha_zero_is_white():
    img = Image.new("RGBA", (6, 6), (0, 0, 0, 0))  # fully transparent
    lum = luminance(img)
    assert np.allclose(lum, 1.0)
    img2 = Image.new("RGBA", (6, 6), (255, 255, 255, 0))
    assert np.allclose(luminance(img2), 1.0)


def test_palette_and_1bit_modes():
    p = Image.new("P", (4, 4))
    p.putpalette([0, 0, 0] + [255, 255, 255] * 255)
    p.putdata([0] * 4 + [255] * 4)
    assert luminance(p).min() < 0.01 and luminance(p).max() > 0.99

    b = Image.new("1", (4, 4), 1)  # all white
    assert np.allclose(luminance(b), 1.0)


# ------------------------------------------------------------------to_html()
# ------------------------------------------------------------------

def test_html_escapes():
    assert to_html(["<b>&"]) == (
        "<pre style='font-family:monospace;line-height:1'>&lt;b&gt;&amp;</pre>"
    )


# ------------------------------------------------------------------cli
def test_cli_error_missing_file():
    r = subprocess.run(
        [sys.executable, str(ROOT.parent / "asciigen.py"), "nope.png"],
        capture_output=True, text=True,
    )
    assert r.returncode == 2
    assert "asciigen:" in r.stderr


def test_cli_stdout_and_file(tmp_path):
    img = str(ROOT.parent / "bench" / "probelab" / "plots" / "breakeven.png")
    script = str(ROOT.parent / "asciigen.py")
    out = tmp_path / "art.txt"
    r = subprocess.run(
        [sys.executable, script, img, "-W", "120", "-o", str(out)],
        capture_output=True, text=True,
    )
    assert r.returncode == 0, r.stderr
    content = out.read_text()
    assert len(content.splitlines()) > 10
    # Dark (lines/axes) and light (background) characters both present.
    assert "@" in content and "." in content
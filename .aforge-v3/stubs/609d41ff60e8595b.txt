"""Gaussian / Laplacian pyramid primitives (block-average coarse-graining).

These are the renormalization-group coarse-graining operators:  a 2x
block average plus (for the Laplacian pyramid) the detail band left
behind.  The generative models in this package share them.
"""

from __future__ import annotations

import numpy as np


def gaussian_pyramid(img: np.ndarray, levels: int) -> list[np.ndarray]:
    """Gaussian pyramid, level 0 = full resolution."""
    pyr = [img.astype(np.float64)]
    cur = img.astype(np.float64)
    for _ in range(levels - 1):
        h = cur.shape[0] // 2
        w = cur.shape[1] // 2
        cur = downsample(cur, h, w)
        pyr.append(cur)
    return pyr


def downsample(img: np.ndarray, h: int, w: int) -> np.ndarray:
    """Area-average downsample (block average = RG coarse-graining)."""
    oh, ow = img.shape
    out = np.empty((h, w))
    for i in range(h):
        r0 = i * oh // h
        r1 = (i + 1) * oh // h
        for j in range(w):
            c0 = j * ow // w
            c1 = (j + 1) * ow // w
            out[i, j] = img[r0:r1, c0:c1].mean()
    return out


def upsample(img: np.ndarray, h: int, w: int) -> np.ndarray:
    """Bilinear upsample via index mapping (no scipy)."""
    oh, ow = img.shape
    ys = np.linspace(0, oh - 1, h)
    xs = np.linspace(0, ow - 1, w)
    y0 = np.floor(ys).astype(int)
    x0 = np.floor(xs).astype(int)
    y1 = np.minimum(y0 + 1, oh - 1)
    x1 = np.minimum(x0 + 1, ow - 1)
    fy = (ys - y0)[:, None]
    fx = (xs - x0)[None, :]
    return (
        img[np.ix_(y0, x0)] * (1 - fy) * (1 - fx)
        + img[np.ix_(y0, x1)] * (1 - fy) * fx
        + img[np.ix_(y1, x0)] * fy * (1 - fx)
        + img[np.ix_(y1, x1)] * fy * fx
    )


def laplacian_bands(img: np.ndarray, levels: int) -> list[np.ndarray]:
    """Laplacian pyramid: band i = pyr[i] - upsample(pyr[i+1]).

    Returns `levels` bands; the last band is the coarsest Gaussian level
    (DC/global structure), the rest are octave detail bands.
    """
    pyr = gaussian_pyramid(img, levels)
    bands = []
    for i in range(levels - 1):
        coarse_up = upsample(pyr[i + 1], pyr[i].shape[0], pyr[i].shape[1])
        bands.append(pyr[i] - coarse_up)
    bands.append(pyr[-1])
    return bands

"""Whittle field — nonstationary multiscale spectral synthesis.

Idea (idea #1 from the ideation list): model an image as a Gaussian
random field whose second-order structure lives in a *local* spectral
measure — a compact per-scale/per-orientation amplitude envelope.  The
Whittle likelihood makes the periodogram the natural estimator, so
"learning" is a few averaged periodograms, and "generation" is one
FFT-filtered draw per scale, O(n log n), no iterative chain.

The nonstationary part: a Laplacian pyramid splits the image into
octave bands; each band is treated as a locally stationary field with
its own learned spectral measure.  A single global PSD cannot represent
an image that is smooth in some places and textured in others; a
per-band PSD can.
"""

from __future__ import annotations

import numpy as np


def _fft2c(a: np.ndarray) -> np.ndarray:
    """Centered 2D FFT."""
    return np.fft.fftshift(np.fft.fft2(np.fft.ifftshift(a)))


def _ifft2c(a: np.ndarray) -> np.ndarray:
    """Centered inverse 2D FFT."""
    return np.fft.fftshift(np.fft.ifft2(np.fft.ifftshift(a)))


def gaussian_pyramid(img: np.ndarray, levels: int) -> list[np.ndarray]:
    """Gaussian pyramid, level 0 = full resolution."""
    pyr = [img.astype(np.float64)]
    cur = img.astype(np.float64)
    for _ in range(levels - 1):
        # 2x downsampling with a small separable blur to avoid aliasing.
        h = cur.shape[0] // 2
        w = cur.shape[1] // 2
        cur = _downsample(cur, h, w)
        pyr.append(cur)
    return pyr


def _downsample(img: np.ndarray, h: int, w: int) -> np.ndarray:
    """Area-average downsample (robust, no scipy dependency)."""
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


def _upsample(img: np.ndarray, h: int, w: int) -> np.ndarray:
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
    (the DC/global structure), the rest are octave detail bands.
    """
    pyr = gaussian_pyramid(img, levels)
    bands = []
    for i in range(levels - 1):
        coarse_up = _upsample(pyr[i + 1], pyr[i].shape[0], pyr[i].shape[1])
        bands.append(pyr[i] - coarse_up)
    bands.append(pyr[-1])
    return bands


def _polar_psd(psd: np.ndarray, n_r: int, n_theta: int) -> np.ndarray:
    """Resample a centered 2D PSD into (n_theta, n_r) log-polar cells.

    This is the *learned local spectral measure*: a compact anisotropic
    representation that discards phase but keeps the amplitude structure
    that matters for texture identity.
    """
    h, w = psd.shape
    cy, cx = h / 2.0, w / 2.0
    r_max = min(cy, cx)
    ys, xs = np.mgrid[0:h, 0:w]
    r = np.sqrt((ys - cy) ** 2 + (xs - cx) ** 2)
    theta = np.arctan2(ys - cy, xs - cx)  # -pi..pi
    theta = (theta + np.pi) % np.pi  # fold to 0..pi (PSD is symmetric)

    # log-spaced radial bins: more resolution where texture detail lives
    r_edges = np.geomspace(max(r[r > 0].min(), 1e-6), r_max, n_r + 1)
    r_idx = np.digitize(r, r_edges) - 1
    r_idx = np.clip(r_idx, 0, n_r - 1)

    theta_edges = np.linspace(0, np.pi, n_theta + 1)
    t_idx = np.digitize(theta, theta_edges) - 1
    t_idx = np.clip(t_idx, 0, n_theta - 1)

    acc = np.zeros((n_theta, n_r))
    cnt = np.zeros((n_theta, n_r))
    np.add.at(acc, (t_idx.ravel(), r_idx.ravel()), psd.ravel())
    np.add.at(cnt, (t_idx.ravel(), r_idx.ravel()), 1.0)
    return acc / np.maximum(cnt, 1.0)


class WhittleField:
    """Learns and samples a per-band spectral measure.

    Attributes:
        levels: pyramid depth
        bands: list of (H, W) shape per band
        measure: list of (n_theta, n_r) learned spectral measures
        means: list of per-band DC means
    """

    def __init__(self, levels: int = 5, n_r: int = 16, n_theta: int = 8):
        self.levels = levels
        self.n_r = n_r
        self.n_theta = n_theta
        self.bands: list[tuple[int, int]] = []
        self.measure: list[np.ndarray] = []
        self.means: list[float] = []

    def fit(self, images: list[np.ndarray]) -> "WhittleField":
        """Learn the spectral measure from a list of same-size images."""
        if not images:
            raise ValueError("need at least one image")
        h0, w0 = images[0].shape
        self.bands = []
        acc = []
        for img in images:
            if img.shape != (h0, w0):
                raise ValueError("all images must share one size")
            bands = laplacian_bands(img, self.levels)
            if not self.bands:
                self.bands = [b.shape for b in bands]
                acc = [np.zeros((self.n_theta, self.n_r)) for _ in bands]
            for k, b in enumerate(bands):
                psd = np.abs(_fft2c(b)) ** 2
                acc[k] += _polar_psd(psd, self.n_r, self.n_theta)

        n = len(images)
        self.measure = [a / n for a in acc]
        self.means = [0.0] * self.levels  # bands are zero-mean by construction

        # Regularize: add a tiny floor so sampling never hits exactly zero
        # and the smallest scales stay numerically stable.
        for k, m in enumerate(self.measure):
            self.measure[k] = m + 1e-12 * m.max()
        return self

    def _sample_band(self, k: int, shape: tuple[int, int], seed: int | None) -> np.ndarray:
        """Sample one band by filtering white noise with the learned measure."""
        h, w = shape
        rng = np.random.default_rng(seed)
        noise = rng.standard_normal((h, w)) + 1j * rng.standard_normal((h, w))
        # reconstruct a full 2D amplitude map from the polar measure
        amp = self._measure_to_amp(self.measure[k], h, w)
        return np.real(_ifft2c(_fft2c(noise) * np.sqrt(amp)))

    def _measure_to_amp(self, measure: np.ndarray, h: int, w: int) -> np.ndarray:
        """Interpolate the polar measure back onto the FFT grid."""
        cy, cx = h / 2.0, w / 2.0
        ys, xs = np.mgrid[0:h, 0:w]
        r = np.sqrt((ys - cy) ** 2 + (xs - cx) ** 2)
        theta = np.arctan2(ys - cy, xs - cx)
        theta = (theta + np.pi) % np.pi

        r_max = min(cy, cx)
        r_norm = r / max(r_max, 1e-9)
        r_norm = np.clip(r_norm, 0.0, 1.0)
        r_idx = r_norm * (self.n_r - 1)
        theta_idx = theta / np.pi * (self.n_theta - 1)

        r_lo = np.floor(r_idx).astype(int)
        r_hi = np.minimum(r_lo + 1, self.n_r - 1)
        t_lo = np.floor(theta_idx).astype(int)
        t_hi = np.minimum(t_lo + 1, self.n_theta - 1)
        fr = r_idx - r_lo
        ft = theta_idx - t_lo

        amp = (
            measure[t_lo, r_lo] * (1 - fr) * (1 - ft)
            + measure[t_lo, r_hi] * (1 - fr) * ft
            + measure[t_hi, r_lo] * fr * (1 - ft)
            + measure[t_hi, r_hi] * fr * ft
        )
        return amp

    def sample(self, shape: tuple[int, int], seed: int | None = None) -> np.ndarray:
        """Generate an image by sampling each band and reconstructing."""
        h, w = shape
        rng = np.random.default_rng(seed)
        # band shapes follow the pyramid geometry of the learned images
        band_shapes = []
        ch, cw = h, w
        for _ in range(self.levels):
            band_shapes.append((ch, cw))
            ch = max(1, ch // 2)
            cw = max(1, cw // 2)

        sampled = []
        for k in range(self.levels):
            s = self._sample_band(k, band_shapes[k], None)
            sampled.append(s)

        # reconstruct: coarse base, then upsample-and-add each finer band
        img = sampled[-1]
        for k in range(self.levels - 2, -1, -1):
            up = _upsample(img, band_shapes[k][0], band_shapes[k][1])
            img = up + sampled[k]
        return img

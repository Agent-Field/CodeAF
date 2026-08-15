"""Renormalization-group (RG) cascade generator.

The physics idea: natural images are approximately *scale invariant*.
Coarse-graining an image by block-averaging (a real-space RG step)
produces a field statistically similar to the original.  Therefore the
*conditional* distribution of fine detail given a coarse field can be
modeled with ONE small local kernel + a noise model, applied at every
scale — "depth" becomes RG iterations instead of a trained deep stack.

Structure:

    fit(images):
        for each training image, compute the Laplacian pyramid.
        Learn, for each band, a single local conditional model
            detail(x) ~ a * filter(coarse neighbors, x) + noise
        i.e. the detail at position x is a small linear function of the
        coarse field around x, plus independent (heavy-tailed) noise.
        Enforce scale invariance by pooling statistics across bands:
        the learned kernel and noise scale are shared, only the noise
        *amplitude* differs per band (the RG beta function).

    sample(coarse):  given a coarse field, fill each scale from the
        bottom up:
            fine = upsample(coarse)
            detail = K * fine + noise(amplitude[level])
            fine += detail
        If no coarse field is given, draw one from the learned coarse
        distribution (fit on the coarsest level of the training data).
"""

from __future__ import annotations

import numpy as np

from genimg.pyramid import gaussian_pyramid, laplacian_bands, upsample


class RGCascade:
    """Scale-invariant coarse-to-fine generator.

    Args:
        levels: number of octaves in the training pyramid
        k: neighborhood radius for the local detail kernel (kernel is (2k+1)^2)
        tol: tiny floor for variance estimates
    """

    def __init__(self, levels: int = 4, k: int = 1, tol: float = 1e-8):
        self.levels = levels
        self.k = k
        self.tol = tol
        # learned: per-level noise amplitude and a shared "beta function"
        self.noise_amp: list[float] = []
        self.amp_ratios: list[float] = []   # amplitude[level] / amplitude[0]
        self.coarse_mean: float = 0.0
        self.coarse_std: float = 1.0
        self.kernel = np.zeros(((2 * k + 1) ** 2,))
        self.kernel_amp: float = 0.0

    # -- RG assumption check ------------------------------------------------
    def check_scale_invariance(self, images: list[np.ndarray]) -> list[float]:
        """Return per-level detail activity std (normalized by coarse std).

        If the data is scale invariant, these values cluster; if not,
        they vary smoothly (the beta function) but the shared-kernel
        approximation still holds reasonably.
        """
        stdevs = []
        for level in range(self.levels - 1):  # skip coarsest
            bands = []
            for img in images:
                b = laplacian_bands(img, self.levels)[level]
                bands.append(b.ravel())
            detail = np.concatenate(bands)
            stdevs.append(detail.std())
        return stdevs

    # -- learning -----------------------------------------------------------
    def fit(self, images: list[np.ndarray]) -> "RGCascade":
        """Learn the shared kernel + per-level noise amplitudes."""
        if images is None or len(images) == 0:
            raise ValueError("need images")
        h0, w0 = images[0].shape
        k = self.k
        km = 2 * k + 1
        km2 = km * km

        # pool all (coarse-neighborhood, detail) pairs across levels
        # and across images; solve the least-squares problem
        #   detail(x) ≈ kernel · coarse_neighborhood(x)
        # in closed form (normal equations).
        A_list, b_list = [], []
        per_level = []
        n_levels = self.levels - 1

        for level in range(n_levels):
            A_l, b_l = [], []
            for img in images:
                bands = laplacian_bands(img, self.levels)
                detail = bands[level]
                coarse = bands[level + 1]
                # upsample coarse to detail's resolution for local sampling
                ch, cw = coarse.shape
                dh, dw = detail.shape
                coarse_up = upsample(coarse, dh, dw)
                A = _neighborhood_matrix(coarse_up, k)  # (N, km2)
                b = detail.ravel()
                A_l.append(A)
                b_l.append(b)
            A = np.concatenate(A_l)
            b = np.concatenate(b_l)
            # closed-form least squares
            AtA = A.T @ A + self.tol * np.eye(km2)
            Atb = A.T @ b
            coef = np.linalg.solve(AtA, Atb)
            pred = A @ coef
            resid = b - pred
            per_level.append((coef, resid.std()))
            A_list.append(A)
            b_list.append(b)

        # average the kernels across levels (the RG shared-kernel ansatz)
        kernel = np.mean([c for c, _ in per_level], axis=0)
        # normalize so the amplitude is comparable
        kernel = kernel / (np.linalg.norm(kernel) + self.tol)
        self.kernel = kernel

        # noise amplitude per level, relative to level 0
        # (fit one overall amplitude, then per-level ratios)
        base_amp = per_level[0][1]
        self.noise_amp = [r for _, r in per_level]
        self.amp_ratios = [r / (base_amp + self.tol) for r in self.noise_amp]

        # coarse-level statistics (for unconditional generation)
        coarse_all = np.concatenate(
            [laplacian_bands(img, self.levels)[-1].ravel() for img in images]
        )
        self.coarse_mean = float(coarse_all.mean())
        self.coarse_std = float(coarse_all.std())

        # combined linear gain per level (kernel + amplitude) — this is the
        # "beta function" of the RG flow, one scalar per scale
        self.gains = [
            self.noise_amp[l] / (np.linalg.norm(kernel) + self.tol)
            for l in range(n_levels)
        ]
        return self

    # -- generation ---------------------------------------------------------
    def sample(self, shape: tuple[int, int], seed: int | None = None) -> np.ndarray:
        """Unconditional generation: draw a coarse field, then RG-flow up."""
        h, w = shape
        rng = np.random.default_rng(seed)
        coarse = self.coarse_mean + self.coarse_std * rng.standard_normal((h, w))
        # block-average down until we have a valid coarse seed
        while max(coarse.shape) > 16:
            coarse = _blockavg2(coarse)
        return self.refine(coarse, shape, seed=seed)

    def refine(self, coarse: np.ndarray, shape: tuple[int, int],
               seed: int | None = None) -> np.ndarray:
        """Coarse-to-fine RG flow: fill detail up to `shape`."""
        rng = np.random.default_rng(seed)
        img = coarse.astype(np.float64)
        n_levels = self.levels - 1
        # iterate until img reaches the target resolution
        steps = 0
        while (img.shape[0] < shape[0] or img.shape[1] < shape[1]) and steps < 12:
            h = min(shape[0], 2 * img.shape[0])
            w = min(shape[1], 2 * img.shape[1])
            fine = upsample(img, h, w)
            # choose the level index: the finest we still have amplitudes for
            lvl = max(0, min(n_levels - 1, steps))
            detail = _apply_kernel(fine, self.kernel, self.k) * self.gains[lvl]
            noise = rng.standard_normal(fine.shape) * self.noise_amp[lvl]
            img = fine + detail + noise
            steps += 1
        return img


def _blockavg2(img: np.ndarray) -> np.ndarray:
    h, w = img.shape
    return img[: 2 * (h // 2), : 2 * (w // 2)].reshape(
        h // 2, 2, w // 2, 2
    ).mean(axis=(1, 3))


def _neighborhood_matrix(img: np.ndarray, k: int) -> np.ndarray:
    """(N, (2k+1)^2) matrix of local neighborhoods of `img`.

    One row per pixel (N = h*w), with zero-padding at the border, so the
    design matrix aligns with `detail.ravel()` (same length).
    """
    h, w = img.shape
    km = 2 * k + 1
    A = np.empty((h * w, km * km))
    padded = np.zeros((h + 2 * k, w + 2 * k))
    padded[k:k + h, k:k + w] = img
    idx = 0
    for i in range(h):
        for j in range(w):
            A[idx] = padded[i:i + km, j:j + km].ravel()
            idx += 1
    return A


def _apply_kernel(img: np.ndarray, kernel: np.ndarray, k: int) -> np.ndarray:
    """Convolve `img` with the (2k+1)^2 kernel, zero-padded."""
    h, w = img.shape
    km = 2 * k + 1
    out = np.zeros_like(img)
    ker = kernel.reshape(km, km)
    for di in range(km):
        for dj in range(km):
            # shift img by (di-k, dj-k) and add weighted
            si = di - k
            sj = dj - k
            shifted = np.zeros_like(img)
            r0, r1 = max(0, -si), min(h, h - si)
            c0, c1 = max(0, -sj), min(w, w - sj)
            if r0 < r1 and c0 < c1:
                shifted[r0:r1, c0:c1] = img[r0 + si:r1 + si, c0 + sj:c1 + sj]
            out += ker[di, dj] * shifted
    return out

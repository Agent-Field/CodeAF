"""Shape + texture generative composition, no neural networks.

The physics/complex-analysis idea:

  * SHAPE is a harmonic map.  A smooth shape is the solution of a
    Poisson/Laplace equation with boundary data (the Dirichlet problem).
    Drawing a bird silhouette, prescribing boundary values (bright head,
    dark wing, mid body), and solving Laplace's equation in the interior
    produces a smooth, topologically clean scalar field that *is* the
    shape.  This is the "conformal skeleton" idea made discrete: the
    Laplace solve is the single expensive step, it is linear and sparse,
    and it guarantees smoothness.

  * TEXTURE is added by a spectral/scale model.  We take the smooth
    field, add oriented detail through a steerable/Gabor-like filter
    bank, and whiten with the learned (or analytic) 1/f spectrum.  This
    is the "statistical flesh" — feathers, grain, fur — without pixels
    being decided by a neural network.

  * MULTISCALE DETAIL comes from the RG cascade (genimg.rg): the shape
    field acts as the coarse seed, and the cascade fills fine structure.
"""

from __future__ import annotations

import numpy as np
from scipy import sparse
from scipy.sparse.linalg import cg

from genimg.pyramid import upsample


def harmonic_field(mask: np.ndarray, boundary: np.ndarray,
                   prescribed: np.ndarray | None = None,
                   tol: float = 1e-8, maxiter: int = 2000) -> np.ndarray:
    """Solve Laplace's equation with boundary data on a masked interior.

    Args:
        mask: (h, w) bool, True = interior (unknowns)
        boundary: (h, w) float, prescribed values; used where mask is False
        prescribed: (h, w) bool, True = interior points with FIXED values
            (Dirichlet seeds, excluded from the solve)
        tol, maxiter: CG tolerances

    Returns:
        (h, w) float field, = boundary outside, harmonic inside.
    """
    h, w = mask.shape
    if prescribed is None:
        prescribed = np.zeros((h, w), dtype=bool)
    unknown = mask & ~prescribed
    interior = np.nonzero(unknown)
    idx = {p: k for k, p in enumerate(zip(*interior))}
    N = len(interior[0])

    rows, cols, vals = [], [], []
    rhs = np.zeros(N)
    for k, (i, j) in enumerate(zip(*interior)):
        rows.append(k); cols.append(k); vals.append(4.0)
        for di, dj in [(-1, 0), (1, 0), (0, -1), (0, 1)]:
            ni, nj = i + di, j + dj
            if 0 <= ni < h and 0 <= nj < w and unknown[ni, nj]:
                rows.append(k); cols.append(idx[(ni, nj)]); vals.append(-1.0)
            else:
                # Dirichlet boundary value (prescribed seed or outside)
                rhs[k] += boundary[ni, nj] if (0 <= ni < h and 0 <= nj < w) else 0.0
    A = sparse.csr_matrix((vals, (rows, cols)), shape=(N, N))
    x, info = cg(A, rhs, rtol=tol, atol=0.0, maxiter=maxiter)
    if info > 0:
        raise RuntimeError(f"CG did not converge (iter={info})")

    out = boundary.copy().astype(np.float64)
    out[interior] = x
    return out


def _gabor_kernel(size: int, freq: float, theta: float, sigma: float) -> np.ndarray:
    """One oriented Gabor-like kernel (real part)."""
    y, x = np.mgrid[-(size // 2):size // 2 + 1, -(size // 2):size // 2 + 1]
    xr = x * np.cos(theta) + y * np.sin(theta)
    yr = -x * np.sin(theta) + y * np.cos(theta)
    g = np.exp(-(xr ** 2 + yr ** 2) / (2 * sigma ** 2)) * np.cos(2 * np.pi * freq * xr)
    return g - g.mean()


def _convolve(img: np.ndarray, ker: np.ndarray) -> np.ndarray:
    """2D correlation with 'same' shape (numpy, no scipy.ndimage)."""
    h, w = img.shape
    kh, kw = ker.shape
    ph, pw = kh // 2, kw // 2
    padded = np.zeros((h + 2 * ph, w + 2 * pw))
    padded[ph:ph + h, pw:pw + w] = img
    out = np.zeros_like(img)
    for i in range(kh):
        for j in range(kw):
            out += ker[i, j] * padded[i:i + h, j:j + w]
    return out


class ShapeTexture:
    """Compose a shape (harmonic field) with statistical texture.

    Args:
        octaves: number of Gabor frequency octaves
        angles: number of orientations per octave
    """

    def __init__(self, octaves: int = 3, angles: int = 4):
        self.octaves = octaves
        self.angles = angles

    def texture(self, shape: np.ndarray, seed: int | None = None,
                strength: float = 1.0) -> np.ndarray:
        """Add oriented, 1/f-colored texture to a smooth shape field."""
        rng = np.random.default_rng(seed)
        h, w = shape.shape
        # white noise, colored 1/f by FFT
        noise = rng.standard_normal((h, w))
        F = np.fft.fftshift(np.fft.fft2(noise))
        ys, xs = np.mgrid[-h // 2:h // 2, -w // 2:w // 2]
        r = np.sqrt(xs ** 2 + ys ** 2) + 1e-6
        F *= 1.0 / r
        colored = np.real(np.fft.ifft2(np.fft.ifftshift(F)))
        colored = colored / colored.std()

        # oriented Gabor energy: pick orientations present in the shape's
        # gradient, so texture follows the form
        gy, gx = np.gradient(shape)
        energy = np.sqrt(gx ** 2 + gy ** 2)
        # dominant local orientation via structure tensor
        ax = gx ** 2 + 1e-9
        ay = gy ** 2 + 1e-9
        ang = 0.5 * np.arctan2(2 * gx * gy, ax - ay)

        tex = np.zeros_like(colored)
        for octave in range(self.octaves):
            freq = 2 ** (octave - self.octaves // 2)
            for a in range(self.angles):
                theta = a * np.pi / self.angles
                ker = _gabor_kernel(11, freq, theta, 2.0)
                # weight by how aligned the kernel is with local structure
                align = np.abs(np.cos(ang - theta))
                resp = _convolve(colored, ker)
                tex += align * resp * freq ** 2  # 1/f amplitude per octave
        tex = tex / (tex.std() + 1e-9)
        # modulate texture by shape's local energy: feathers where edges are
        mod = 1.0 + strength * (energy / (energy.max() + 1e-9))
        return shape + tex * mod * 0.35


def make_bird_mask(h: int = 128, w: int = 160) -> tuple[np.ndarray, np.ndarray]:
    """A parametric bird silhouette as a boolean mask + boundary labels.

    Returns (mask, boundary): mask is True inside the bird; boundary
    carries prescribed field values (head bright, wing dark, beak dark,
    eye bright, background dark).  The interior seeds make the harmonic
    field vary smoothly across the body instead of being flat.
    """
    ys, xs = np.mgrid[0:h, 0:w]
    # body: a tilted ellipse
    cx, cy = w * 0.50, h * 0.58
    a, b = w * 0.30, h * 0.24
    theta = 0.10
    xr = (xs - cx) * np.cos(theta) + (ys - cy) * np.sin(theta)
    yr = -(xs - cx) * np.sin(theta) + (ys - cy) * np.cos(theta)
    body = (xr / a) ** 2 + (yr / b) ** 2 <= 1.0

    # head: a circle at the front-top
    hx, hy, hr = w * 0.80, h * 0.30, h * 0.14
    head = (xs - hx) ** 2 + (ys - hy) ** 2 <= hr ** 2

    # beak: a wedge pointing right
    bx, by = w * 0.90, h * 0.29
    beak = (xs >= bx) & (ys >= by - h * 0.025) & (ys <= by + h * 0.05)

    # tail: a triangle to the back-left
    tx, ty = w * 0.16, h * 0.62
    tail = (
        (xs >= tx - w * 0.10) & (xs <= tx + w * 0.10)
        & (ys >= ty - h * 0.06) & (ys <= ty + h * 0.12)
        & (np.abs((ys - ty) * 0.6 - (xs - tx) * 0.4) < w * 0.06)
    )

    mask = body | head | beak | tail
    prescribed = np.zeros((h, w), dtype=bool)

    # boundary / interior seed values
    # region values live on the *silhouette edge* (Dirichlet); the
    # interior of each region is solved smoothly by Laplace.
    boundary = np.full((h, w), 0.30)  # background dark
    eroded = _erode(mask)
    edge = mask & ~eroded
    boundary[edge] = 0.45
    # region-specific edge values
    head_edge = head & edge
    boundary[head_edge] = 0.85
    body_edge = body & edge & ~head
    boundary[body_edge] = 0.55
    beak_edge = beak & edge
    boundary[beak_edge] = 0.25
    tail_edge = tail & edge
    boundary[tail_edge] = 0.40

    # interior seeds (Dirichlet points inside the region so the harmonic
    # field carries shape structure, not just the silhouette)
    eye_x, eye_y, eye_r = w * 0.80, h * 0.27, h * 0.025
    eye = (xs - eye_x) ** 2 + (ys - eye_y) ** 2 <= eye_r ** 2
    boundary[eye] = 0.05  # bright eye
    prescribed[eye] = True

    wing_x, wing_y = w * 0.50, h * 0.58
    wing = (np.abs(ys - wing_y) < h * 0.10) & (np.abs(xs - wing_x) < w * 0.18) & mask
    boundary[wing] = 0.35  # darker wing band
    prescribed[wing] = True

    # prescribe the full interior: region interiors carry their region
    # value, so the harmonic solve only interpolates a thin transition
    # band near the silhouette edge (smooth, shape-respecting field).
    inner = eroded & ~prescribed
    boundary[inner] = 0.50  # neutral body interior
    boundary[inner & head] = 0.80
    boundary[inner & beak] = 0.28
    boundary[inner & tail] = 0.38
    prescribed[inner] = True

    return mask, boundary, prescribed


def _erode(mask: np.ndarray) -> np.ndarray:
    """Boolean erosion by one pixel (4-neighbour)."""
    h, w = mask.shape
    out = mask.copy()
    for di, dj in [(-1, 0), (1, 0), (0, -1), (0, 1)]:
        shifted = np.zeros_like(mask)
        r0, r1 = max(0, -di), min(h, h - di)
        c0, c1 = max(0, -dj), min(w, w - dj)
        if r0 < r1 and c0 < c1:
            shifted[r0:r1, c0:c1] = mask[r0 + di:r1 + di, c0 + dj:c1 + dj]
        out &= shifted
    return out


if __name__ == "__main__":
    import time
    t0 = time.time()
    mask, boundary, prescribed = make_bird_mask(128, 160)
    field = harmonic_field(mask, boundary, prescribed)
    st = ShapeTexture(octaves=3, angles=4)
    img = st.texture(field, seed=42)
    img = (img - img.min()) / (img.max() - img.min() + 1e-9)
    from PIL import Image
    Image.fromarray((img * 255).astype(np.uint8)).save("/tmp/bird_harmonic.png")
    print(f"wrote /tmp/bird_harmonic.png in {time.time()-t0:.1f}s")

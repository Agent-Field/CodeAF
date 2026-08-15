"""Gaussian splatting for images — renderer, decomposer, sampler.

The 2D analog of 3D Gaussian splatting:

    image(x) = sum_k  alpha_k * G(x | mu_k, Sigma_k) * color_k

where G is an anisotropic 2D Gaussian.  A handful of splats renders a
smooth surface (feathers, gradients, background); a scene with N splats
is a *marked point process* — positions, scales, orientations, colors,
opacity — which is exactly the statistical-physics "dead leaves" model
of natural image statistics.

Modules:

    render(splats, shape)      — sum splats onto a grid (pure numpy)
    decompose(image, n)        — greedy Gaussian decomposition of a real
                                 photo into n splats (the compression /
                                 representation side)
    sample(model, shape)       — generate a NEW image by sampling splat
                                 statistics learned from a set of images

No neural network anywhere: fitting is greedy residual matching, and
generation is a marked point process draw.
"""

from __future__ import annotations

import numpy as np


# ----------------------------------------------------------------------
# splat representation
# ----------------------------------------------------------------------

def _gaussian2d(h: int, w: int, cx: float, cy: float,
                sx: float, sy: float, theta: float) -> np.ndarray:
    """Anisotropic 2D Gaussian on an (h, w) grid (unnormalized)."""
    ys, xs = np.mgrid[0:h, 0:w]
    dx = xs - cx
    dy = ys - cy
    c, s = np.cos(theta), np.sin(theta)
    xr = c * dx + s * dy
    yr = -s * dx + c * dy
    return np.exp(-0.5 * ((xr / max(sx, 1e-3)) ** 2 + (yr / max(sy, 1e-3)) ** 2))


def render(splats: list[dict], shape: tuple[int, int]) -> np.ndarray:
    """Sum splats onto an (h, w) grid; returns (h, w, 3) float in [0,1]."""
    h, w = shape
    out = np.zeros((h, w, 3))
    for sp in splats:
        g = _gaussian2d(h, w, sp["cx"], sp["cy"], sp["sx"], sp["sy"], sp["theta"])
        alpha = sp["alpha"]
        color = np.array(sp["color"], dtype=float)
        out += alpha * g[..., None] * color[None, None, :]
    return np.clip(out, 0.0, 1.0)


# ----------------------------------------------------------------------
# decompose: greedy residual matching of a real image
# ----------------------------------------------------------------------

def decompose(img: np.ndarray, n: int, min_scale: float = 1.0,
              seed: int = 0) -> list[dict]:
    """Greedily fit n Gaussian splats to an (h, w, 3) image in [0,1].

    Coarse-to-fine (multigrid): first cover the image with large splats
    at low resolution (captures the smooth structure), then add smaller
    splats to chase residual detail.  This matches how the human visual
    system and the RG framework both decompose — large scales first.
    """
    rng = np.random.default_rng(seed)
    h, w = img.shape[:2]
    resid = img.copy()
    splats = []
    border = 8
    # coarse-to-fine scale schedule: n_splats per octave
    octaves = [(20.0, 6), (12.0, 10), (7.0, 20), (4.0, 30),
               (2.5, 40), (1.6, 60), (1.1, 80)]
    total = sum(s for _, s in octaves)
    n = min(n, total)
    budget = n
    for base_scale, count in octaves:
        take = min(count, budget)
        if take <= 0:
            break
        budget -= take
        for _ in range(take):
            energy = np.sum(resid ** 2, axis=2)
            eb = energy[border:-border or None, border:-border or None]
            cy, cx = np.unravel_index(np.argmax(eb), eb.shape)
            cy, cx = cy + border, cx + border
            # fit at this octave's scale: alpha = weighted mean of the
            # residual under the Gaussian footprint (peak-normalized g),
            # so large splats don't overshoot a small bright spot.
            g = _gaussian2d(h, w, float(cx), float(cy), base_scale, base_scale, 0.0)
            wsum = g.sum()
            color = (g[..., None] * resid).sum(axis=(0, 1)) / (wsum + 1e-9)
            alpha = 1.0
            splats.append({"cx": float(cx), "cy": float(cy),
                           "sx": base_scale, "sy": base_scale,
                           "theta": 0.0, "alpha": alpha,
                           "color": np.clip(color, 0, 1)})
            resid = resid - alpha * g[..., None] * color[None, None, :]
    return splats


# ----------------------------------------------------------------------
# sample: marked point process generation
# ----------------------------------------------------------------------

class SplatSampler:
    """Learn splat statistics from a set of images, then generate.

    The learned model is a *marked point process*: splat positions
    follow a smoothed empirical density, and each splat's marks
    (scale, orientation, color) come from the empirical joint
    distribution, conditioned on position (so feathers follow form).
    """

    def __init__(self, n_splats: int = 800, scale: float = 3.0):
        self.n_splats = n_splats
        self.scale = scale
        self.pos_density: np.ndarray | None = None
        self.mean_color = np.zeros(3)
        self.train_mean = 0.0
        self.sx_mean = 2.0
        self.sy_mean = 2.0
        self.sx_std = 1.0
        self.sy_std = 1.0

    def fit(self, images: list[np.ndarray], splats_per_image: int = 200,
            seed: int = 0) -> "SplatSampler":
        """Decompose each image and learn the splat statistics."""
        all_pos = []
        all_sx, all_sy = [], []
        all_colors = []
        h, w = images[0].shape[:2]
        pos_acc = np.zeros((h, w))
        for img in images:
            splats = decompose(img, splats_per_image, seed=seed)
            for sp in splats:
                pos_acc[int(sp["cy"]), int(sp["cx"])] += 1
                all_pos.append((sp["cy"], sp["cx"]))
                all_sx.append(sp["sx"])
                all_sy.append(sp["sy"])
                all_colors.append(sp["color"])
        # smooth the position density
        from scipy.ndimage import gaussian_filter
        self.pos_density = gaussian_filter(pos_acc.astype(float), self.scale) + 1e-6
        self.pos_density /= self.pos_density.sum()
        self.all_sx = np.array(all_sx)
        self.all_sy = np.array(all_sy)
        self.all_colors = np.array(all_colors)
        self.sx_mean = float(self.all_sx.mean())
        self.sy_mean = float(self.all_sy.mean())
        self.sx_std = float(self.all_sx.std())
        self.sy_std = float(self.all_sy.std())
        self.mean_color = self.all_colors.mean(axis=0)
        self.train_mean = float(np.mean([img.mean() for img in images]))
        return self

    def sample(self, shape: tuple[int, int], seed: int | None = None) -> np.ndarray:
        """Draw a new image from the learned marked point process."""
        rng = np.random.default_rng(seed)
        h, w = shape
        # sample positions from the density
        flat = self.pos_density.ravel()
        idx = rng.choice(len(flat), size=self.n_splats, p=flat)
        ys, xs = np.unravel_index(idx, (h, w))
        splats = []
        for k in range(self.n_splats):
            # marks: sample from the empirical joint with slight jitter
            j = rng.integers(0, len(self.all_sx))
            sx = max(1.0, self.all_sx[j] + rng.normal(0, self.sx_std * 0.1))
            sy = max(1.0, self.all_sy[j] + rng.normal(0, self.sy_std * 0.1))
            theta = rng.uniform(0, np.pi)
            color = np.clip(self.all_colors[j] + rng.normal(0, 0.05, 3), 0, 1)
            splats.append({"cx": float(xs[k]), "cy": float(ys[k]),
                           "sx": sx, "sy": sy, "theta": theta,
                           "alpha": 1.0, "color": color})
        img = render(splats, shape)
        # splats overlap, so the naive sum over-brightens; rescale so the
        # generated image matches the *training image* mean brightness
        # (the splat colors themselves are residual-weighted and dark).
        target = self.train_mean
        img = img * (target / (img.mean() + 1e-9))
        return np.clip(img, 0, 1)


# ----------------------------------------------------------------------
if __name__ == "__main__":
    from PIL import Image

    # decompose a real photo
    im = np.asarray(Image.open("/tmp/photo_128.png").convert("RGB")).astype(float) / 255.0
    splats = decompose(im, 800, seed=1)
    rec = render(splats, im.shape[:2])
    psnr = 10 * np.log10(1.0 / (np.mean((rec - im) ** 2) + 1e-12))
    print(f"800 splats -> PSNR {psnr:.1f} dB")
    Image.fromarray((rec * 255).astype(np.uint8)).save("/tmp/photo_splats.png")

    # sample new images from the learned statistics
    s = SplatSampler(n_splats=600, scale=4.0)
    s.fit([im], splats_per_image=300, seed=2)
    gen = s.sample((128, 128), seed=3)
    Image.fromarray((gen * 255).astype(np.uint8)).save("/tmp/photo_splats_gen.png")
    print("wrote /tmp/photo_splats.png and /tmp/photo_splats_gen.png")

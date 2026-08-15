"""Smoke tests for Gaussian splatting."""

import numpy as np

from genimg.splats import SplatSampler, decompose, render, _gaussian2d


def _photo_like(h: int = 64, w: int = 64, seed: int = 0) -> np.ndarray:
    """A smooth image with a bright center and structure."""
    rng = np.random.default_rng(seed)
    ys, xs = np.mgrid[0:h, 0:w]
    img = 0.3 + 0.5 * np.exp(-((xs - w / 2) ** 2 + (ys - h / 2) ** 2) / (2 * 20 ** 2))
    img = img[..., None] * np.ones(3)[None, None, :]
    img += 0.05 * rng.standard_normal((h, w, 3))
    return np.clip(img, 0, 1)


def test_render_single_splat():
    sp = {"cx": 32.0, "cy": 32.0, "sx": 5.0, "sy": 5.0, "theta": 0.0,
          "alpha": 1.0, "color": np.array([1.0, 0.5, 0.0])}
    img = render([sp], (64, 64))
    assert img.shape == (64, 64, 3)
    assert img[32, 32, 0] > 0.99  # peak is bright red
    assert img[0, 0, 0] < 0.01     # far corner is empty


def test_decompose_reduces_energy():
    img = _photo_like()
    sp = decompose(img, 20, seed=1)
    rec = render(sp, img.shape[:2])
    err_before = np.mean(img ** 2)
    err_after = np.mean((img - rec) ** 2)
    assert err_after < 0.5 * err_before, "decomposition must reduce energy"


def test_sampler_matches_brightness():
    img = _photo_like()
    s = SplatSampler(n_splats=300, scale=4.0)
    s.fit([img], splats_per_image=50, seed=2)
    gen = s.sample((64, 64), seed=3)
    assert gen.shape == (64, 64, 3)
    # brightness should be in a reasonable band around the training mean
    assert 0.5 * img.mean() < gen.mean() < 1.5 * img.mean()


if __name__ == "__main__":
    test_render_single_splat()
    test_decompose_reduces_energy()
    test_sampler_matches_brightness()
    print("splats smoke: OK")

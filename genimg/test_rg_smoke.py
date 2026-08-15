"""Smoke tests for the RG cascade."""

import numpy as np

from genimg.rg import RGCascade
from genimg.pyramid import laplacian_bands, upsample


def make_texture(h: int, w: int, scale: float = 4.0) -> np.ndarray:
    """A smooth-ish random texture with a 1/f-ish spectrum."""
    rng = np.random.default_rng(0)
    noise = rng.standard_normal((h, w))
    # smooth via repeated block averaging (acts as a low-pass)
    f = noise
    for _ in range(2):
        f = f[:2 * (f.shape[0] // 2), :2 * (f.shape[1] // 2)].reshape(
            f.shape[0] // 2, 2, f.shape[1] // 2, 2).mean(axis=(1, 3))
        f = upsample(f, 2 * f.shape[0], 2 * f.shape[1]) if f.shape[0] * 2 <= h else f
    f = upsample(f, h, w)
    return f / f.std()


def test_pyramid_reconstruction():
    img = np.random.rand(64, 64)
    bands = laplacian_bands(img, 4)
    rec = bands[-1]
    for k in range(2, -1, -1):
        rec = upsample(rec, bands[k].shape[0], bands[k].shape[1]) + bands[k]
    assert np.allclose(rec, img, atol=1e-9)


def test_rg_assumption_similar_across_levels():
    imgs = [make_texture(64, 64) for _ in range(8)]
    rg = RGCascade(levels=4, k=1)
    stdevs = rg.check_scale_invariance(imgs)
    assert len(stdevs) == 3
    # scale-invariance: per-level detail stdev should not blow up / vanish
    r = max(stdevs) / (min(stdevs) + 1e-12)
    assert r < 10, f"detail stdevs too spread: {stdevs}"


def test_fit_and_sample_shape():
    imgs = [make_texture(64, 64) for _ in range(8)]
    rg = RGCascade(levels=4, k=1)
    rg.fit(imgs)
    s = rg.sample((64, 64), seed=1)
    assert s.shape == (64, 64)
    assert np.isfinite(s).all()
    assert s.std() > 1e-3


def test_refine_from_coarse():
    imgs = [make_texture(64, 64) for _ in range(8)]
    rg = RGCascade(levels=4, k=1)
    rg.fit(imgs)
    coarse = np.random.rand(16, 16)
    fine = rg.refine(coarse, (64, 64), seed=2)
    assert fine.shape == (64, 64)
    # coarse structure should be preserved at fine resolution
    assert abs(fine.mean() - coarse.mean()) < 1.0


if __name__ == "__main__":
    test_pyramid_reconstruction()
    test_rg_assumption_similar_across_levels()
    test_fit_and_sample_shape()
    test_refine_from_coarse()
    print("RG smoke: OK")

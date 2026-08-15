"""Quick smoke test for the Whittle field."""

import numpy as np
from PIL import Image

from genimg.whittle import WhittleField, laplacian_bands, gaussian_pyramid


def make_checker(h: int, w: int, cell: int) -> np.ndarray:
    img = np.zeros((h, w))
    for i in range(0, h, cell):
        for j in range(0, w, cell):
            if (i // cell + j // cell) % 2 == 0:
                img[i:i + cell, j:j + cell] = 1.0
    return img


def make_stripes(h: int, w: int, cell: int) -> np.ndarray:
    img = np.zeros((h, w))
    for i in range(0, h, cell):
        if (i // cell) % 2 == 0:
            img[i:i + cell, :] = 1.0
    return img


def test_pyramid_shapes():
    img = np.random.rand(64, 64)
    pyr = gaussian_pyramid(img, 4)
    assert [p.shape for p in pyr] == [(64, 64), (32, 32), (16, 16), (8, 8)]
    bands = laplacian_bands(img, 4)
    assert len(bands) == 4
    # bands reconstruct the image
    rec = bands[-1]
    for k in range(2, -1, -1):
        from genimg.whittle import _upsample
        rec = _upsample(rec, bands[k].shape[0], bands[k].shape[1]) + bands[k]
    assert rec.shape == (64, 64)
    assert np.allclose(rec, img, atol=1e-9), "Laplacian pyramid must be invertible"


def test_learn_and_sample():
    rng = np.random.default_rng(0)
    imgs = [make_checker(64, 64, 8) for _ in range(4)]
    imgs += [make_stripes(64, 64, 8) for _ in range(4)]

    field = WhittleField(levels=4, n_r=8, n_theta=4)
    field.fit(imgs)
    sample = field.sample((64, 64), seed=1)
    assert sample.shape == (64, 64)
    assert np.isfinite(sample).all()
    assert sample.std() > 1e-3, "sample should not be constant"


if __name__ == "__main__":
    test_pyramid_shapes()
    test_learn_and_sample()
    print("Whittle smoke: OK")

    # Visual demo
    imgs = [make_checker(96, 96, 12) for _ in range(6)]
    field = WhittleField(levels=5, n_r=12, n_theta=6)
    field.fit(imgs)
    s = field.sample((96, 96), seed=7)
    # normalize to 0..255
    s = (s - s.min()) / (s.max() - s.min() + 1e-9)
    Image.fromarray((s * 255).astype(np.uint8)).save("/tmp/whittle_checker.png")
    print("wrote /tmp/whittle_checker.png")

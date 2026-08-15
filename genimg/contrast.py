"""Contrast/histogram matching for generated faces.

The eigenface base averages away extreme pixel values (SVD is a
mean-reconstruction), so generated faces cluster in mid-gray and look
washed out / X-ray-like.  Real faces have a wider, more peaked
histogram with deep blacks and bright highlights.

Fix: learn the *cumulative distribution* of real face pixels, then
remap generated pixels through the inverse-CDF (histogram matching).
This is a monotone, per-pixel, O(n) operation — no learning, no NN.
"""

from __future__ import annotations

import numpy as np


def learn_histogram(images: np.ndarray, bins: int = 256) -> tuple[np.ndarray, np.ndarray]:
    """Learn the pixel CDF from a set of images in [0,1].

    Returns (bin_edges, cdf) where cdf[i] = fraction of pixels <= bin_edges[i].
    """
    all_px = np.concatenate([img.ravel() for img in images])
    hist, edges = np.histogram(all_px, bins=bins, range=(0, 1))
    cdf = np.cumsum(hist).astype(float)
    cdf = cdf / (cdf[-1] + 1e-12)
    return edges, cdf


def match_histogram(img: np.ndarray, edges: np.ndarray, cdf: np.ndarray) -> np.ndarray:
    """Remap img through the learned CDF (histogram matching).

    For each pixel value v, find the target value t such that
    CDF_real(t) = CDF_img(v).  Since we want the *generated* image to
    have the real distribution, we map v -> t where t = CDF_real^-1(CDF_img(v)).
    But CDF_img is unknown; instead we directly map v -> t by
    t = edges[argmin |CDF_real - v|]... no.  Correct approach:
    t = CDF_real^-1(rank(v) / N) where rank is within the image.
    """
    # rank of each pixel within the image (0..1)
    flat = img.ravel()
    order = np.argsort(flat)
    ranks = np.empty_like(flat)
    ranks[order] = np.arange(len(flat)) / (len(flat) - 1)
    # target value = inverse CDF of the rank
    target = np.interp(ranks, cdf, edges[:-1])
    return target.reshape(img.shape)


def match_histogram_fast(img: np.ndarray, edges: np.ndarray, cdf: np.ndarray) -> np.ndarray:
    """Proper histogram matching: map img's CDF onto the target CDF.

    For each source value v, find t such that CDF_img(v) = CDF_real(t).
    We estimate CDF_img from the image's own histogram, then invert.
    """
    flat = img.ravel()
    # source CDF (256 bins over [0,1])
    hist_src, _ = np.histogram(flat, bins=256, range=(0, 1))
    cdf_src = np.cumsum(hist_src).astype(float)
    cdf_src = cdf_src / (cdf_src[-1] + 1e-12)
    src_vals = np.linspace(0, 1, 256)
    # for each source bin, target = CDF_real^-1(CDF_src(bin))
    target = np.interp(cdf_src, cdf, edges[:-1])
    idx = np.clip((img * 255).astype(int), 0, 255)
    return target[idx]


if __name__ == "__main__":
    import pickle, glob
    from PIL import Image
    from genimg.eigenface import load_orl

    X, files = load_orl()
    real = X.reshape(400, 56, 46)
    edges, cdf = learn_histogram(real)

    with open("/tmp/sparseface.pkl", "rb") as f:
        sf = pickle.load(f)

    for seed in [7, 8, 9]:
        g = sf.generate(seed=seed).reshape(56, 46)
        g2 = match_histogram_fast(g, edges, cdf)
        print(f"seed {seed}: gen std {g.std():.3f} -> matched std {g2.std():.3f} "
              f"(real {real.std():.3f})")
        Image.fromarray((np.clip(g2, 0, 1) * 255).astype(np.uint8)).save(
            f"/tmp/face_contrast_{seed}.png"
        )
    print("saved face_contrast_7..9.png")

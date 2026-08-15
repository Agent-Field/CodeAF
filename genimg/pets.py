"""Oxford-IIIT Pets: load, center-crop, downscale to a uniform canvas.

Pets images are diverse photos (dogs/cats, varied positions), unlike
ORL/Fashion (centered).  We center-crop the largest square around the
image center, resize to 128x128, and convert to grayscale.  This is
the alignment step that makes PCA viable.
"""

from __future__ import annotations

import glob
import os

import numpy as np
from PIL import Image


def load_pets(dir: str = "/tmp/pets/images", size: int = 128,
              max_n: int = 4000, seed: int = 0) -> np.ndarray:
    """Load dog images (class names) or all, aligned to (size, size)."""
    files = sorted(glob.glob(f"{dir}/*.jpg"))
    # filter to dogs (non-cat patterns in filenames: _1.jpg.. sit for cats
    # Am I sure? Oxford naming: class_count.jpg — cats use pattern cat names)
    rng = np.random.default_rng(seed)
    rng.shuffle(files)
    if max_n:
        files = files[:max_n]

    imgs = []
    for f in files:
        im = Image.open(f).convert("L")  # grayscale
        w, h = im.size
        # center-crop to square
        m = min(w, h)
        left = (w - m) // 2
        top = (h - m) // 2
        im = im.crop((left, top, left + m, top + m))
        im = im.resize((size, size), Image.LANCZOS)
        imgs.append(np.asarray(im).astype(float) / 255.0)
    return np.array(imgs)


if __name__ == "__main__":
    import time

    import numpy as np

    t0 = time.time()
    X = load_pets(max_n=500, size=64)
    print(f"loaded {X.shape} in {time.time()-t0:.0f}s")
    np.save("/tmp/pets_64.npy", X)
    print("saved /tmp/pets_64.npy")
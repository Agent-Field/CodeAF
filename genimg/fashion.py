"""Fashion-MNIST loader + category-agnostic generator pipeline.

Fashion-MNIST: 60K grayscale 28x28 clothing images, 10 categories,
already centered/standardized like ORL — perfect for the eigenface +
K-SVD + contrast hybrid without alignment work.
"""

from __future__ import annotations

import gzip
import pickle
import time

import numpy as np


def load_fashion(dir: str = "/tmp") -> tuple[np.ndarray, np.ndarray]:
    """Load Fashion-MNIST (train). Returns (images [60000,28,28], labels)."""
    def read(path_frag: str) -> np.ndarray:
        with gzip.open(path_frag, "rb") as f:
            magic = int.from_bytes(f.read(4), "big")
            ndim = magic & 0xFF
            shape = tuple(int.from_bytes(f.read(4), "big") for _ in range(ndim))
            return np.frombuffer(f.read(), dtype=np.uint8).reshape(shape)

    imgs = read(f"{dir}/fashion_train.ubyte.gz")
    labels = read(f"{dir}/fashion_train_labels.gz")
    return imgs, labels


load_fidx = load_fashion


def load_test(dir: str = "/tmp") -> tuple[np.ndarray, np.ndarray]:
    with gzip.open(f"{dir}/fashion_test_images.gz", "rb") as f:
        magic = int.from_bytes(f.read(4), "big")
        ndim = magic & 0xFF
        shape = tuple(int.from_bytes(f.read(4), "big") for _ in range(ndim))
        imgs = np.frombuffer(f.read(), dtype=np.uint8).reshape(shape)
    with gzip.open(f"{dir}/fashion_test_labels.gz", "rb") as f:
        magic = int.from_bytes(f.read(4), "big")
        ndim = magic & 0xFF
        shape = tuple(int.from_bytes(f.read(4), "big") for _ in range(ndim))
        labels = np.frombuffer(f.read(), dtype=np.uint8).reshape(shape)
    return imgs, labels


def train_class(dir: str, cls: int, n: int = 6000) -> np.ndarray:
    """Images of one class, normalized [0,1], (n, 28*28)."""
    imgs, labels = load_fidx(dir)
    sel = imgs[labels == cls][:n].astype(float) / 255.0
    return sel.reshape(sel.shape[0], -1)


if __name__ == "__main__":
    imgs, labels = load_fidx()
    print("loaded", imgs.shape, "counts:", {d: int((labels == d).sum()) for d in range(10)})

    # pick class 0 = T-shirt/top
    for cls in [0, 1]:
        X = train_class("/tmp", cls, n=6000)
        print(f"class {cls}: {X.shape}")

        # quick eigenface on a subset
        from genimg.eigenface import EigenFace
        t0 = time.time()
        ef = EigenFace(k=60).fit(X[:2000])
        ev = ef.explained_variance()
        print(f"  eigenface: {time.time()-t0:.1f}s, top-60 explains {np.cumsum(ev)[59]*100:.1f}%")
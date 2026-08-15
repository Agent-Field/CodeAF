"""MNIST loader (no dependencies beyond numpy/PIL)."""

from __future__ import annotations

import gzip
from pathlib import Path

import numpy as np


def _read_idx(path: Path) -> np.ndarray:
    """Read an IDX-format file (gzip optional)."""
    with gzip.open(path, "rb") if path.name.endswith(".gz") else open(path, "rb") as f:
        magic = int.from_bytes(f.read(4), "big")
        ndim = magic & 0xFF
        dtype = {0x08: np.uint8, 0x09: np.int8, 0x0B: np.int16,
                 0x0C: np.int32, 0x0D: np.float32, 0x0E: np.float64}[magic >> 8]
        shape = tuple(int.from_bytes(f.read(4), "big") for _ in range(ndim))
        return np.frombuffer(f.read(), dtype=dtype).reshape(shape)


def load_mnist(data_dir: str | Path, kind: str = "train") -> tuple[np.ndarray, np.ndarray]:
    """Return (images [m,28,28] uint8, labels [m])."""
    data_dir = Path(data_dir)
    prefix = "train" if kind == "train" else "t10k"
    imgs = _read_idx(data_dir / f"{prefix}-images-idx3-ubyte.gz")
    labels = _read_idx(data_dir / f"{prefix}-labels-idx1-ubyte.gz")
    return imgs, labels


def binarize(imgs: np.ndarray, thr: float = 0.5) -> np.ndarray:
    """Threshold 0..1 images to binary."""
    return (imgs > thr).astype(np.int64)


def downscale(img: np.ndarray, size: int) -> np.ndarray:
    """Area-average an image to size x size."""
    h, w = img.shape
    out = np.empty((size, size))
    for i in range(size):
        r0, r1 = i * h // size, (i + 1) * h // size
        for j in range(size):
            c0, c1 = j * w // size, (j + 1) * w // size
            out[i, j] = img[r0:r1, c0:c1].mean()
    return out

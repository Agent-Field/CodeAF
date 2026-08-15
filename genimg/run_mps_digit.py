"""Train MPS on real MNIST digits and generate samples.

Demonstrates idea #4 end-to-end: learn a tensor-network probability
distribution over binary digit images, then sample new digits with no
neural network — just exact ancestral sampling of the learned MPS.
"""

from __future__ import annotations

import time
from pathlib import Path

import numpy as np
from PIL import Image

from genimg.mnist import binarize, downscale, load_mnist
from genimg.mps import PositiveMPS


def train_digit(digit: int, size: int = 16, n_train: int = 400,
                bond: int = 6, epochs: int = 120, lr: float = 0.1):
    """Train an MPS on one digit class, return the model + training images."""
    imgs, labels = load_mnist("data")
    sel = imgs[labels == digit][:n_train]
    # downscale to size x size, threshold to binary
    X = np.array([binarize(downscale(s.astype(float) / 255, size)) for s in sel])
    # snake ordering: 2D locality -> short 1D correlation length
    from genimg.snake import snake_flat_to_chain
    X = snake_flat_to_chain(X.reshape(X.shape[0], -1), size, size)
    print(f"digit {digit}: {X.shape[0]} train images, {X.shape[1]} sites, "
          f"bond={bond}")
    model = PositiveMPS(X.shape[1], r=bond)
    t0 = time.time()
    model.fit(X, epochs=epochs, lr=lr, verbose=False)
    print(f"  trained in {time.time()-t0:.1f}s, "
          f"avg log-lik={model.log_likelihood(X):.2f}")
    return model, X


def generate(model: PositiveMPS, size: int, m: int = 64, seed: int = 0):
    """Sample m images and stack them into a grid for viewing."""
    S = model.sample(m, seed=seed)
    # un-snake back to row-major 2D
    from genimg.snake import chain_to_snake_flat
    imgs = chain_to_snake_flat(S, size, size).reshape(m, size, size)
    # montage: 8x8 grid
    grid_w = 8
    grid_h = (m + grid_w - 1) // grid_w
    montage = np.zeros((grid_h * size, grid_w * size), dtype=np.uint8)
    for k in range(m):
        r, c = k // grid_w, k % grid_w
        montage[r * size:(r + 1) * size, c * size:(c + 1) * size] = (
            imgs[k] * 255
        )
    return Image.fromarray(montage, mode="L")


if __name__ == "__main__":
    import sys

    digit = int(sys.argv[1]) if len(sys.argv) > 1 else 0
    size = int(sys.argv[2]) if len(sys.argv) > 2 else 16
    model, X = train_digit(digit, size=size)
    img = generate(model, size, m=64, seed=42)
    out = f"/tmp/mps_digit_{digit}.png"
    img.save(out)
    print(f"wrote {out}")

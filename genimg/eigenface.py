"""Data-driven face generation: eigenfaces + latent sampling (no NN).

Idea (idea A from the ideation, made concrete): aligned faces live on a
low-dimensional manifold (~40-100 intrinsic dims).  PCA on the real ORL
faces finds that manifold; sampling in the PCA latent and decoding gives
*photometric* faces — real face structure, not a hand-authored schematic.

Pipeline:
    1. center the faces, SVD -> eigenfaces
    2. project training faces -> latent coefficients (m x k)
    3. fit a Gaussian per latent dimension
    4. generate: sample latent, decode to pixels, uncenter
    5. bonus: morphs (latent-space interpolation between real faces)

All classical linear algebra (SVD + Gaussian sampling).  No neural net.
"""

from __future__ import annotations

import glob

import numpy as np
from PIL import Image


def load_orl(globb: str = "/tmp/orl/orl_faces/*/*.pgm",
             size: tuple[int, int] = (56, 46)) -> tuple[np.ndarray, list[str]]:
    """Load ORL faces, downscale to (h, w), return (N, h*w) float [0,1] + paths."""
    files = sorted(glob.glob(globb))
    imgs = []
    for f in files:
        im = Image.open(f).convert("L").resize((size[1], size[0]))
        imgs.append(np.asarray(im).astype(float) / 255.0)
    X = np.array(imgs)  # (N, h, w)
    N, h, w = X.shape
    return X.reshape(N, h * w), files


class EigenFace:
    """PCA face model with Gaussian latent sampling."""

    def __init__(self, k: int = 80):
        self.k = k
        self.mean = None
        self.components = None  # (k, d) eigenfaces
        self.latent_mean = None
        self.latent_std = None

    def fit(self, X: np.ndarray) -> "EigenFace":
        """X: (N, d) float [0,1].  Fit PCA via SVD of the centered data."""
        self.mean = X.mean(axis=0)
        Xc = X - self.mean
        # SVD: Xc = U S Vt, V rows are eigenfaces
        U, S, Vt = np.linalg.svd(Xc, full_matrices=False)
        k = min(self.k, Vt.shape[0])
        self.components = Vt[:k]  # (k, d)
        # project training data onto latent
        latent = Xc @ self.components.T  # (N, k)
        self.latent_mean = latent.mean(axis=0)
        self.latent_std = latent.std(axis=0)
        self._U = U
        self._S = S
        self.k = k
        return self

    def explained_variance(self) -> np.ndarray:
        """Fraction of total variance captured by each component."""
        s2 = self._S ** 2
        return s2 / (s2.sum() + 1e-12)

    def project(self, x: np.ndarray) -> np.ndarray:
        """Map a face (d,) to latent (k,)."""
        return (x - self.mean) @ self.components.T

    def decode(self, z: np.ndarray) -> np.ndarray:
        """Map latent (k,) to pixels (d,)."""
        return self.mean + z @ self.components

    def sample(self, n: int = 1, seed: int | None = None) -> np.ndarray:
        """Sample new faces from the Gaussian latent."""
        rng = np.random.default_rng(seed)
        Z = rng.normal(self.latent_mean, self.latent_std, size=(n, self.k))
        return np.array([self.decode(z) for z in Z])

    def morph(self, i: int, j: int, t: np.ndarray) -> np.ndarray:
        """Morph between training faces i, j at fractions t in [0,1]."""
        zi = self.project(i)
        zj = self.project(j)
        return np.array([self.decode((1 - tt) * zi + tt * zj) for tt in t])

    def interpolate(self, z1: np.ndarray, z2: np.ndarray, t: np.ndarray) -> np.ndarray:
        """Morph between two latent vectors at fractions t."""
        return np.array([self.decode((1 - tt) * z1 + tt * z2) for tt in t])


def to_img(x: np.ndarray, h: int = 56, w: int = 46) -> Image.Image:
    x = np.clip(x, 0, 1)
    return Image.fromarray((x.reshape(h, w) * 255).astype(np.uint8))


if __name__ == "__main__":
    X, files = load_orl()
    print(f"loaded {X.shape[0]} faces, {X.shape[1]} dims")
    ef = EigenFace(k=100).fit(X)
    ev = ef.explained_variance()
    cum = np.cumsum(ev)
    print(f"top-100 explain {cum[99]*100:.1f}% of variance")
    print(f"top-50 explain {cum[49]*100:.1f}%")

    # generate 8 new faces
    gen = ef.sample(8, seed=42)
    faces = [to_img(g) for g in gen]
    mont = Image.new("L", (46 * 8, 56))
    for i, im in enumerate(faces):
        mont.paste(im, (i * 46, 0))
    mont.save("/tmp/eigen_gen.png")
    print("saved /tmp/eigen_gen.png")

    # morph between two real faces
    morphed = ef.morph(0, 30, np.linspace(0, 1, 8))
    mfaces = [to_img(g) for g in morphed]
    mmont = Image.new("L", (46 * 8, 56))
    for i, im in enumerate(mfaces):
        mmont.paste(im, (i * 46, 0))
    mmont.save("/tmp/eigen_morph.png")
    print("saved /tmp/eigen_morph.png")

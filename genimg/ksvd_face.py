"""Sparse-detail faces: eigenface base + K-SVD residual dictionary.

The linear PCA base (eigenfaces) gives a coherent face but smooths away
high-frequency identity detail (skin texture, hair edges, glasses).
K-SVD learns an overcomplete dictionary of *residual patches* from the
400 real ORL faces; a sparse combination of atoms adds the sharp detail
PCA loses — nonlinearly, with no neural network.

Pipeline:
    1. eigenface base (SVD, as in eigenface.py)
    2. per-position residual patch dictionary via K-SVD
       (classical: OMP sparse coding + SVD dictionary update)
    3. generate: sample PCA latent -> base face; per patch position,
       sample a sparse code from the *positional* code statistics of the
       training faces -> residual detail; overlap-add
"""

from __future__ import annotations

import numpy as np


# ----------------------------------------------------------------------
# K-SVD dictionary learning (classical, no NN)
# ----------------------------------------------------------------------

class KSVD:
    """Overcomplete sparse dictionary: Y ~= D X with ||x_i||_0 <= T.

    Args:
        K: number of atoms (dictionary columns)
        T: sparsity (atoms per patch)
        seed: RNG seed
    """

    def __init__(self, K: int = 256, T: int = 8, seed: int = 0):
        self.K = K
        self.T = T
        self.rng = np.random.default_rng(seed)
        self.D = None

    def fit(self, Y: np.ndarray, iterations: int = 15) -> "KSVD":
        """Learn dictionary.  Y: (d, N) patches as columns."""
        d, N = Y.shape
        # init with random training patches (normalized)
        idx = self.rng.choice(N, size=self.K, replace=False)
        D = Y[:, idx].copy()
        norms = np.linalg.norm(D, axis=0)
        D = D / (norms + 1e-12)

        for it in range(iterations):
            X = self.encode(Y, D)
            D = self._update(Y, D, X)
        self.D = D
        return self

    def encode(self, Y: np.ndarray, D: np.ndarray | None = None) -> np.ndarray:
        """OMP sparse coding.  Y: (d, N); returns codes X: (K, N)."""
        if D is None:
            D = self.D
        d, N = Y.shape
        K = D.shape[1]
        X = np.zeros((K, N))
        R = Y.copy()
        for n in range(N):
            active = []
            r = R[:, n]
            for _ in range(self.T):
                # pick atom most correlated with residual
                corr = D.T @ r
                j = int(np.argmax(np.abs(corr)))
                if j in active:
                    break
                active.append(j)
                # least squares on active atoms
                A = D[:, active]
                coef, *_ = np.linalg.lstsq(A, Y[:, n], rcond=None)
                r = Y[:, n] - A @ coef
            if active:
                A = D[:, active]
                coef, *_ = np.linalg.lstsq(A, Y[:, n], rcond=None)
                X[active, n] = coef
        return X

    def _update(self, Y: np.ndarray, D: np.ndarray, X: np.ndarray) -> np.ndarray:
        """Approximate K-SVD update: one least-squares solve for all atoms.

        The exact K-SVD does a per-atom SVD (slow, O(K) SVDs per
        iteration).  The approximate version fixes the support (active
        atoms per patch) and solves the full dictionary by least squares
        in one shot — an order of magnitude faster, nearly identical
        reconstruction quality.
        """
        # least squares: min ||Y - D X||_F^2 over D, support fixed
        D = Y @ X.T @ np.linalg.inv(X @ X.T + 1e-6 * np.eye(X.shape[0]))
        # normalize atoms (scale absorbed into codes)
        norms = np.linalg.norm(D, axis=0)
        D = D / (norms + 1e-12)
        return D

    def decode(self, X: np.ndarray) -> np.ndarray:
        """Reconstruct patches from codes: (d, N)."""
        return self.D @ X


# ----------------------------------------------------------------------
# face generator: eigenface base + sparse residual
# ----------------------------------------------------------------------

def extract_patches(img: np.ndarray, p: int, stride: int) -> tuple[np.ndarray, list[tuple[int, int]]]:
    """Extract overlapping p x p patches.  img: (h, w) float [0,1].

    Returns (patches (p*p, N) as columns, list of (row, col) positions).
    """
    h, w = img.shape
    positions = []
    for r in range(0, h - p + 1, stride):
        for c in range(0, w - p + 1, stride):
            positions.append((r, c))
    N = len(positions)
    patches = np.zeros((p * p, N))
    for n, (r, c) in enumerate(positions):
        patches[:, n] = img[r:r + p, c:c + p].ravel()
    return patches, positions


def reassemble(patches: np.ndarray, positions: list[tuple[int, int]],
               h: int, w: int, p: int) -> np.ndarray:
    """Overlap-add patches back into an image (averaging overlaps)."""
    acc = np.zeros((h, w))
    cnt = np.zeros((h, w))
    for n, (r, c) in enumerate(positions):
        acc[r:r + p, c:c + p] += patches[:, n].reshape(p, p)
        cnt[r:r + p, c:c + p] += 1
    return acc / (cnt + 1e-9)


class SparseFace:
    """Eigenface base + per-position K-SVD residual detail."""

    def __init__(self, k_eig: int = 80, K: int = 256, T: int = 8,
                 p: int = 8, stride: int = 4, seed: int = 0):
        self.k_eig = k_eig
        self.K = K
        self.T = T
        self.p = p
        self.stride = stride
        self.seed = seed
        self.ef = None  # EigenFace
        self.ksvd = None
        self.h = self.w = 0
        self.pos: list[tuple[int, int]] = []
        # positional code statistics
        self.pos_atoms: list[np.ndarray] = []   # activation frequency per atom
        self.pos_coef_mean: list[np.ndarray] = []
        self.pos_coef_std: list[np.ndarray] = []

    def fit(self, X: np.ndarray) -> "SparseFace":
        """X: (N, d) full faces float [0,1], d = h*w."""
        from genimg.eigenface import EigenFace
        N, d = X.shape
        # infer h, w (ratio ~ 1.22 for ORL 56x46)
        self.h = int(round(np.sqrt(d * 1.22)))
        self.w = d // self.h
        while self.h * self.w != d:
            self.h -= 1
            self.w = d // self.h

        # 1. eigenface base
        self.ef = EigenFace(k=self.k_eig).fit(X)
        resid_all = X - self.ef.decode(self.ef.project(X))

        # 2. patch dictionary on the *residual* faces
        all_patches = []
        for i in range(N):
            face = resid_all[i].reshape(self.h, self.w)
            patches, self.pos = extract_patches(face, self.p, self.stride)
            all_patches.append(patches)
        Y = np.hstack(all_patches)
        self.ksvd = KSVD(K=self.K, T=self.T, seed=self.seed).fit(Y)

        # 3. positional code statistics
        npos = len(self.pos)
        # per_pos[j]: (p*p, N) — one patch per face at position j
        per_pos = []
        for j in range(npos):
            stacked = np.zeros((self.p * self.p, N))
            for i in range(N):
                stacked[:, i] = all_patches[i][:, j]
            per_pos.append(stacked)
        self.pos_atoms = []
        self.pos_coef_mean = []
        self.pos_coef_std = []
        for j in range(npos):
            codes = self.ksvd.encode(per_pos[j])
            # activation frequency
            freq = (codes != 0).mean(axis=1)
            means = codes.mean(axis=1)
            stds = codes.std(axis=1)
            self.pos_atoms.append(freq)
            self.pos_coef_mean.append(means)
            self.pos_coef_std.append(stds)
        return self

    def generate(self, seed: int | None = None, detail: float = 1.0) -> np.ndarray:
        """Sample a new face: PCA latent + sparse residual detail."""
        rng = np.random.default_rng(seed)
        # base
        z = rng.normal(self.ef.latent_mean, self.ef.latent_std)
        base = self.ef.decode(z).reshape(self.h, self.w)

        # residual: per position, sample a sparse code from stats
        npos = len(self.pos)
        patches = np.zeros((self.p * self.p, npos))
        for j in range(npos):
            freq = self.pos_atoms[j]
            mean = self.pos_coef_mean[j]
            std = self.pos_coef_std[j] + 1e-6
            # sample active atoms: Bernoulli with activation probability
            active = rng.random(self.K) < freq
            if not active.any():
                # keep at least the most frequent atom
                active[np.argmax(freq)] = True
            coef = np.zeros(self.K)
            coef[active] = rng.normal(mean[active], std[active])
            patches[:, j] = self.ksvd.D @ coef
        resid = reassemble(patches, self.pos, self.h, self.w, self.p)
        face = base + detail * resid
        return np.clip(face, 0, 1).ravel()


if __name__ == "__main__":
    from genimg.eigenface import load_orl
    X, files = load_orl()
    print(f"loaded {X.shape[0]} faces, {X.shape[1]} dims")
    sf = SparseFace(k_eig=80, K=256, T=8, p=8, stride=4).fit(X)
    print("fitted K-SVD dictionary + positional stats")

    from PIL import Image
    faces = []
    for s in range(8):
        g = sf.generate(seed=s).reshape(sf.h, sf.w)
        faces.append(Image.fromarray((np.clip(g, 0, 1) * 255).astype(np.uint8)))
    mont = Image.new("L", (sf.w * 8, sf.h))
    for i, im in enumerate(faces):
        mont.paste(im, (i * sf.w, 0))
    mont.resize((sf.w * 8 * 3, sf.h * 3), Image.NEAREST).save("/tmp/sparse_gen.png")
    print("saved /tmp/sparse_gen.png")

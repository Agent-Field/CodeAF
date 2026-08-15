"""Matrix-Product-State (MPS) generative model.

Idea (#4 from the ideation list): model an image as a tensor-network
probability distribution over flattened pixels.

    p(x) = B_0[x_0] B_1[x_1] ... B_{n-1}[x_{n-1}]   (positive MPS)

with bond dimension `r`.  This is the positive (real) special case of
the Born machine: for real non-negative amplitudes psi the squared
Born rule |psi|^2 factorizes into the same product with bond dimension
r^2, so a positive MPS of bond r is exactly a Born machine with real
amplitudes of bond sqrt(r).

Why it is efficient: natural images have mostly *local* correlation, so
the bipartite entanglement (measured by the bond dimension needed) is
tiny compared to n.  The model compresses the distribution by a factor
roughly n / r, and both learning and exact sampling cost O(n r^2) per
sweep/sample — no Markov chain, no gradient backprop through depth.

Learning is exact maximum-likelihood by gradient ascent; the gradient
with respect to a single core has a closed form in terms of left/right
sums (transfer operators) and is verified by finite differences.
"""

from __future__ import annotations

import numpy as np


class PositiveMPS:
    """A positive matrix-product-state probability model over binary sites.

    Args:
        n: number of sites (flattened pixels)
        r: bond dimension (compression rank)
        eps: floor kept on core entries (nonnegativity + stability)
    """

    def __init__(self, n: int, r: int = 8, eps: float = 1e-6):
        self.n = n
        self.r = r
        self.d = 2
        self.eps = eps
        self.cores = [self._init_core(i) for i in range(n)]

    # -- geometry ---------------------------------------------------------
    def _left_dim(self, i: int) -> int:
        # bond between site i-1 and i: min(r, states on left, states on right)
        return 1 if i == 0 else min(self.r, self.d ** i, self.d ** (self.n - i))

    def _right_dim(self, i: int) -> int:
        # bond between site i and i+1
        return 1 if i == self.n - 1 else min(
            self.r, self.d ** (i + 1), self.d ** (self.n - i - 1)
        )

    def _init_core(self, i: int) -> np.ndarray:
        d, l, r = self.d, self._left_dim(i), self._right_dim(i)
        c = np.random.uniform(0.5, 1.0, size=(d, l, r))
        return c / (c.sum() + 1e-9)

    # -- exact contraction -------------------------------------------------
    def _left_sum(self) -> list[np.ndarray]:
        """left_sum[i]: length l_i, sum over all prefix configs 0..i-1."""
        sums = [np.ones(1)]
        for i in range(self.n):
            # sums[-1] is a row of length l_i; contract with site i
            s = np.einsum("l,xlr->r", sums[-1], self.cores[i])
            sums.append(s)
        return sums

    def _right_sum(self) -> list[np.ndarray]:
        """right_sum[i]: length l_i, sum over all suffix configs i..n-1."""
        sums = [None] * (self.n + 1)
        sums[self.n] = np.ones(1)
        for i in range(self.n - 1, -1, -1):
            sums[i] = np.einsum("xlr,r->l", self.cores[i], sums[i + 1])
        return sums

    def _partition(self) -> float:
        return float(self._left_sum()[-1][0])

    # -- per-sample environments ------------------------------------------
    def _sample_envs(self, x: np.ndarray) -> tuple[list[np.ndarray], list[np.ndarray]]:
        """Left rows L[i] (length l_i) and right suffix sums S[i] (length l_i).

        L[i] = product of cores 0..i-1 at the sampled values.
        S[i] = product of cores i..n-1 at the sampled values (column).
        """
        L = [np.ones(1)]
        for i in range(self.n):
            L.append(np.einsum("l,lr->r", L[-1], self.cores[i][x[i]]))

        S = [None] * (self.n + 1)
        S[self.n] = np.ones(1)
        for i in range(self.n - 1, -1, -1):
            S[i] = self.cores[i][x[i]] @ S[i + 1]
        return L, S

    def _log_proba(self, x: np.ndarray) -> float:
        L, S = self._sample_envs(x)
        ptilde = float(L[0] @ S[0])
        return float(np.log(ptilde + 1e-30) - np.log(self._partition() + 1e-30))

    # -- gradient ----------------------------------------------------------
    def _gradient(self, X: np.ndarray) -> list[np.ndarray]:
        """Exact gradient of average log-likelihood wrt each core.

        Returns list of (d, l, r) arrays matching self.cores.
        """
        m = X.shape[0]
        Z = self._partition()
        Ls = self._left_sum()
        Rs = self._right_sum()

        grads = [np.zeros_like(c) for c in self.cores]

        for k in range(m):
            x = X[k]
            L, S = self._sample_envs(x)
            ptilde = float(L[0] @ S[0])
            for i in range(self.n):
                # data term: 1[x_i=a] * outer(L_i, S_{i+1}) / ptilde
                g = np.outer(L[i], S[i + 1]) / (ptilde + 1e-30)
                grads[i][x[i]] += g

        for i in range(self.n):
            grads[i] /= m
            # model term: (1/Z) * outer(L_sum_i, R_sum_{i+1}) — same for all a
            model = np.outer(Ls[i], Rs[i + 1]) / (Z + 1e-30)
            grads[i] -= model[None, :, :]

        return grads

    # -- learning ----------------------------------------------------------
    def fit(self, X: np.ndarray, epochs: int = 50, lr: float = 0.05,
            verbose: bool = True) -> "PositiveMPS":
        """Maximum-likelihood gradient ascent.

        X: (m, n) binary array of flattened training images.
        """
        X = np.asarray(X, dtype=np.int64)
        assert X.shape[1] == self.n
        for e in range(epochs):
            grads = self._gradient(X)
            for i in range(self.n):
                self.cores[i] = self.cores[i] + lr * grads[i]
                self.cores[i] = np.clip(self.cores[i], self.eps, None)
                # keep the scale tame; rescaling one core leaves p invariant
                mx = self.cores[i].max()
                if mx > 1.0:
                    self.cores[i] /= mx

            if verbose and (e % 10 == 0 or e == epochs - 1):
                ll = self.log_likelihood(X)
                print(f"  epoch {e:3d}: avg log-lik = {ll:.4f}")
        return self

    def log_likelihood(self, X: np.ndarray) -> float:
        return float(np.mean([self._log_proba(x) for x in X]))

    # -- sampling ----------------------------------------------------------
    def sample(self, m: int = 1, seed: int | None = None) -> np.ndarray:
        """Exact ancestral sampling of m configurations."""
        rng = np.random.default_rng(seed)
        out = np.zeros((m, self.n), dtype=np.int64)
        Rs = self._right_sum()
        L = np.ones((m, 1))
        for i in range(self.n):
            # conditional p(x_i=a | x_<i) ∝ L @ B_i[a] @ R_future
            logits = np.empty((m, self.d))
            for a in range(self.d):
                amp = np.einsum("ml,lr,r->m", L, self.cores[i][a], Rs[i + 1])
                logits[:, a] = np.log(amp + 1e-30)
            logits -= logits.max(axis=1, keepdims=True)
            probs = np.exp(logits)
            probs /= probs.sum(axis=1, keepdims=True)
            x = np.array([rng.choice(self.d, p=p) for p in probs])
            out[:, i] = x
            L = np.einsum("ml,mlr->mr", L, self.cores[i][x])
        return out


def _check_gradient(n: int = 8, r: int = 3, seed: int = 0):
    """Finite-difference check of _gradient against log_likelihood."""
    rng = np.random.default_rng(seed)
    X = (rng.random((32, n)) < 0.5).astype(np.int64)
    model = PositiveMPS(n, r)
    grads = model._gradient(X)

    h = 1e-5
    for i in [0, n // 2, n - 1]:
        core = model.cores[i]
        d, l, rr = core.shape
        a = 1
        p, q = 0, 0
        if l > 1 and rr > 1:
            p, q = 1, 1
        old = core[a, p, q]
        core[a, p, q] = old + h
        llp = model.log_likelihood(X)
        core[a, p, q] = old - h
        llm = model.log_likelihood(X)
        core[a, p, q] = old
        fd = (llp - llm) / (2 * h)
        an = grads[i][a, p, q]
        print(f"  site {i}: analytic={an:.6f} finite_diff={fd:.6f} "
              f"rel_err={abs(an-fd)/max(abs(fd),1e-9):.2e}")
        assert abs(an - fd) < 1e-3 * max(1.0, abs(fd))


if __name__ == "__main__":
    _check_gradient()
    print("MPS gradient check: OK")

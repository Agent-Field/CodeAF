"""Matrix-Product-State (MPS) generative model, log-domain.

Idea (#4 from the ideation list): model an image as a tensor-network
probability distribution over flattened pixels.

    p(x) = B_0[x_0] B_1[x_1] ... B_{n-1}[x_{n-1}]   (positive MPS)

with bond dimension `r`.  This is the positive (real) special case of
the Born machine: a real non-negative amplitude psi of bond r squared
gives a positive MPS of bond r^2, so this model is a Born machine with
real amplitudes.

All computations are in the log domain: cores are stored as logarithms,
partition/forward/backward messages use logsumexp, and the gradient of
the average log-likelihood with respect to the log-cores is

    dL/d log B_i[a,l,r] = E_data[ q_k(l,r) * 1[x_i=a] ]
                          - E_model[ (a,l,r) marginal ]

where q_k is the forward-backward posterior over bond indices for
training sample k.  Both terms are proper probabilities in [0,1], so
the whole routine is stable for arbitrarily long chains (n=784 MNIST
pixels, no underflow), unlike the naive linear-domain product.
"""

from __future__ import annotations

import numpy as np


def _logsumexp(a: np.ndarray, axis: int | tuple[int, ...]) -> np.ndarray:
    """Numerically stable log-sum-exp along the given axis(es)."""
    m = a.max(axis=axis, keepdims=True)
    # guard against -inf
    m = np.where(np.isfinite(m), m, 0.0)
    out = m.squeeze(axis=axis)
    if isinstance(axis, tuple):
        out = np.squeeze(m, axis=axis)
    s = np.log(np.exp(a - m).sum(axis=axis))
    return out + s


class PositiveMPS:
    """Positive MPS over binary sites (d=2), learned by exact ML.

    Args:
        n: number of sites (flattened pixels)
        r: bond dimension (compression rank)
        eps_log: log floor on cores (positivity + stability)
    """

    def __init__(self, n: int, r: int = 6, eps_log: float = -9.0):
        self.n = n
        self.r = r
        self.d = 2
        self.eps_log = eps_log
        self.logcores = [self._init_logcore(i) for i in range(n)]

    # -- geometry ---------------------------------------------------------
    def _left_dim(self, i: int) -> int:
        return 1 if i == 0 else min(self.r, self.d ** i, self.d ** (self.n - i))

    def _right_dim(self, i: int) -> int:
        return 1 if i == self.n - 1 else min(
            self.r, self.d ** (i + 1), self.d ** (self.n - i - 1)
        )

    def _init_logcore(self, i: int) -> np.ndarray:
        d, l, r = self.d, self._left_dim(i), self._right_dim(i)
        # small random log-weights; positive and near-uniform
        return np.log(np.random.uniform(0.7, 1.0, size=(d, l, r)))

    # -- log-domain messages ----------------------------------------------
    def _left_logsum(self) -> list[np.ndarray]:
        """ls[i]: log of the left sum over prefix configs, length l_i."""
        ls = [np.zeros(1)]
        for i in range(self.n):
            lc = self.logcores[i]  # (d, l, r)
            l = ls[-1][None, :, None]  # (1, l, 1)
            v = l + lc  # (d, l, r)
            ls.append(_logsumexp(v, axis=(0, 1)))
        return ls

    def _right_logsum(self) -> list[np.ndarray]:
        """rs[i]: log of the right sum over suffix configs, length l_i."""
        rs = [None] * (self.n + 1)
        rs[self.n] = np.zeros(1)
        for i in range(self.n - 1, -1, -1):
            lc = self.logcores[i]  # (d, l, r)
            r = rs[i + 1][None, None, :]  # (1, 1, r)
            v = lc + r  # (d, l, r)
            rs[i] = _logsumexp(v, axis=(0, 2))
        return rs

    def _log_partition(self) -> float:
        return float(self._left_logsum()[-1][0])

    # -- forward/backward for a batch of samples --------------------------
    def _forward(self, X: np.ndarray) -> tuple[list[np.ndarray], np.ndarray]:
        """Forward log-messages f_i (m, l_i) for each sample; f_0 = 0.

        Returns the list of f_i for i=0..n, and the log-unormalized mass
        log ptilde (m,).
        """
        m = X.shape[0]
        f = [np.zeros((m, 1))]
        for i in range(self.n):
            lc = self.logcores[i]  # (d, l, r)
            xi = X[:, i]  # (m,)
            # select the row for each sample: (m, l, r)
            sel = lc[xi]
            # f_{i+1}[k, r] = logsumexp_l (f_i[k,l] + sel[k,l,r])
            v = f[-1][:, :, None] + sel  # (m, l, r)
            f.append(_logsumexp(v, axis=1))
        log_ptilde = f[-1][:, 0]
        return f, log_ptilde

    def _backward(self, X: np.ndarray) -> tuple[list[np.ndarray], np.ndarray]:
        """Backward log-messages b_i (m, r_i) for each sample; b_n = 0."""
        m = X.shape[0]
        b = [None] * (self.n + 1)
        b[self.n] = np.zeros((m, 1))
        for i in range(self.n - 1, -1, -1):
            lc = self.logcores[i]  # (d, l, r)
            xi = X[:, i]
            sel = lc[xi]  # (m, l, r)
            # b_i[k, l] = logsumexp_r (sel[k,l,r] + b_{i+1}[k,r])
            v = sel + b[i + 1][:, None, :]  # (m, l, r)
            b[i] = _logsumexp(v, axis=2)
        log_ptilde = b[0][:, 0]
        return b, log_ptilde

    # -- probability -------------------------------------------------------
    def _log_proba(self, x: np.ndarray) -> float:
        """log p(x) for a single configuration."""
        f, log_ptilde = self._forward(x[None, :])
        return float(log_ptilde[0] - self._log_partition())

    def log_likelihood(self, X: np.ndarray) -> float:
        f, log_ptilde = self._forward(X)
        return float(np.mean(log_ptilde - self._log_partition()))

    # -- gradient ----------------------------------------------------------
    def _gradient(self, X: np.ndarray) -> list[np.ndarray]:
        """Exact gradient of average log-likelihood wrt each log-core.

        Returns list of (d, l, r) arrays, one per site.
        """
        m = X.shape[0]
        ls = self._left_logsum()
        rs = self._right_logsum()
        logZ = float(ls[-1][0])
        f, log_ptilde = self._forward(X)
        b, _ = self._backward(X)

        grads = [np.zeros_like(lc) for lc in self.logcores]

        for i in range(self.n):
            lc = self.logcores[i]  # (d, l, r)
            d, l, r = lc.shape
            xi = X[:, i]  # (m,)

            # ---- model marginal: P_model(a,l,r) ----
            # exp(ls_i[l] + lc[a,l,r] + rs_{i+1}[r] - logZ)
            log_model = (ls[i][None, :, None] + lc
                         + rs[i + 1][None, None, :] - logZ)  # (d,l,r)
            model = np.exp(log_model)

            # ---- data term: posterior q_k(l,r) for samples with x_i=a ----
            data = np.zeros_like(lc)
            for a in range(d):
                mask = (xi == a)
                if not mask.any():
                    continue
                # q_k[l,r] = exp(f_i[k,l] + lc[a,l,r] + b_{i+1}[k,r] - log_ptilde_k)
                logq = (f[i][mask][:, :, None] + lc[a][None, :, :]
                        + b[i + 1][mask][:, None, :]
                        - log_ptilde[mask][:, None, None])  # (n_a, l, r)
                data[a] = np.exp(logq).sum(axis=0)

            grads[i] = data / m - model

        return grads

    # -- learning ----------------------------------------------------------
    def fit(self, X: np.ndarray, epochs: int = 100, lr: float = 0.5,
            verbose: bool = True) -> "PositiveMPS":
        """Exact maximum-likelihood gradient ascent on log-cores.

        Kept for the finite-difference check and as a reference; use
        :meth:`fit_em` for actual training (closed-form M-step, much
        faster convergence).
        """
        X = np.asarray(X, dtype=np.int64)
        assert X.shape[1] == self.n
        for e in range(epochs):
            grads = self._gradient(X)
            for i in range(self.n):
                self.logcores[i] = self.logcores[i] + lr * grads[i]
                # keep weights bounded below (positivity) and tame the scale
                self.logcores[i] = np.clip(self.logcores[i], self.eps_log, 0.0)

            if verbose and (e % 10 == 0 or e == epochs - 1):
                ll = self.log_likelihood(X)
                print(f"  epoch {e:3d}: avg log-lik = {ll:.4f}")
        return self

    def fit_em(self, X: np.ndarray, sweeps: int = 20, floor: float = 1e-6,
               verbose: bool = True) -> "PositiveMPS":
        """Baum–Welch EM for a positive MPS.

        A positive MPS over a 1D chain is a position-dependent HMM: the
        bond indices are latent states.  The EM M-step has a closed form
        (generalized Baum–Welch):

            B_i[a,l,r] <- E_q[ 1[x_i=a] 1[z_{i-1}=l, z_i=r] ]

        which is exactly the posterior-weighted count `data[a,l,r]`
        computed by :meth:`_gradient`, normalized to sum to one per core.
        No learning rate, monotone log-likelihood, and an order of
        magnitude fewer iterations than gradient ascent.
        """
        X = np.asarray(X, dtype=np.int64)
        assert X.shape[1] == self.n
        m = X.shape[0]

        for s in range(sweeps):
            f, log_ptilde = self._forward(X)
            b, _ = self._backward(X)

            for i in range(self.n):
                lc = self.logcores[i]  # (d, l, r)
                d, l, r = lc.shape
                xi = X[:, i]

                # posterior counts: for each sample, weight the core's
                # (a,l,r) entry by q(l,r | x) for its observed x_i.
                counts = np.zeros_like(lc)
                for a in range(d):
                    mask = (xi == a)
                    if not mask.any():
                        continue
                    logq = (f[i][mask][:, :, None] + lc[a][None, :, :]
                            + b[i + 1][mask][:, None, :]
                            - log_ptilde[mask][:, None, None])
                    counts[a] = np.exp(logq).sum(axis=0)

                # normalize the core to a conditional P(a, r | l): each
                # left-index slice l sums to 1 over (a, r).
                new = np.maximum(counts, floor)
                denom = new.sum(axis=(0, 2), keepdims=True)  # (1, l, 1)
                self.logcores[i] = np.log(new / denom)

            if verbose and (s % 5 == 0 or s == sweeps - 1):
                ll = self.log_likelihood(X)
                print(f"  EM sweep {s:3d}: avg log-lik = {ll:.4f}")
        return self

    # -- sampling ----------------------------------------------------------
    def sample(self, m: int = 1, seed: int | None = None) -> np.ndarray:
        """Exact ancestral sampling via the Born rule."""
        rng = np.random.default_rng(seed)
        rs = self._right_logsum()
        out = np.zeros((m, self.n), dtype=np.int64)
        f = np.zeros((m, 1))  # forward mass for sampled prefix

        for i in range(self.n):
            lc = self.logcores[i]  # (d, l, r)
            # log P(x_i=a | prefix) ∝ sum_{l,r} exp(f[k,l] + lc[a,l,r] + rs_{i+1}[r])
            logits = np.empty((m, self.d))
            for a in range(self.d):
                v = (f[:, :, None] + lc[a][None, :, :]
                     + rs[i + 1][None, None, :])  # (m, l, r)
                logits[:, a] = _logsumexp(v, axis=(1, 2))
            logits -= logits.max(axis=1, keepdims=True)
            probs = np.exp(logits)
            probs /= probs.sum(axis=1, keepdims=True)
            x = np.array([rng.choice(self.d, p=p) for p in probs])
            out[:, i] = x
            # advance forward mass for the chosen x
            sel = lc[x]  # (m, l, r)
            f = _logsumexp(f[:, :, None] + sel, axis=1)

        return out


def _check_gradient(n: int = 8, r: int = 3, seed: int = 0):
    """Finite-difference check of _gradient against log_likelihood."""
    rng = np.random.default_rng(seed)
    X = (rng.random((64, n)) < 0.5).astype(np.int64)
    model = PositiveMPS(n, r)
    grads = model._gradient(X)

    h = 1e-5
    for i in [0, n // 2, n - 1]:
        core = model.logcores[i]
        d, l, rr = core.shape
        a, p, q = 0, 0, 0
        old = core[a, p, q]
        core[a, p, q] = old + h
        llp = model.log_likelihood(X)
        core[a, p, q] = old - h
        llm = model.log_likelihood(X)
        core[a, p, q] = old
        fd = (llp - llm) / (2 * h)
        an = grads[i][a, p, q]
        rel = abs(an - fd) / max(abs(fd), 1e-9)
        print(f"  site {i}: analytic={an:.6f} finite_diff={fd:.6f} "
              f"rel_err={rel:.2e}")
        assert rel < 1e-3


if __name__ == "__main__":
    _check_gradient()
    print("MPS gradient check: OK")

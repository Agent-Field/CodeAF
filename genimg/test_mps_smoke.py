"""MPS learning + sampling smoke test on a structured prior."""

import numpy as np

from genimg.mps import PositiveMPS


def make_data(n: int, m: int, seed: int = 0) -> np.ndarray:
    """Binary images: first half of sites = 1, second half = 0."""
    rng = np.random.default_rng(seed)
    X = np.zeros((m, n), dtype=np.int64)
    X[:, : n // 2] = 1
    # add a small amount of label noise to be realistic
    flip = rng.random((m, n)) < 0.05
    X = np.where(flip, 1 - X, X)
    return X


def test_learns_prior():
    n, m = 16, 200
    X = make_data(n, m)
    model = PositiveMPS(n, r=3)
    before = model.log_likelihood(X)
    model.fit(X, epochs=60, lr=0.1, verbose=False)
    after = model.log_likelihood(X)
    assert after > before, f"learning must raise log-lik ({before:.2f} -> {after:.2f})"
    # generated samples should be mostly top-half ones
    S = model.sample(100, seed=1)
    frac_top = S[:, : n // 2].mean()
    frac_bot = S[:, n // 2 :].mean()
    assert frac_top > 0.6, f"top half should be mostly 1, got {frac_top:.2f}"
    assert frac_bot < 0.4, f"bottom half should be mostly 0, got {frac_bot:.2f}"


def test_exact_marginals_small():
    """For n=4, training modes must outrank random configurations."""
    n = 4
    X = np.array([[1, 1, 0, 0], [0, 0, 1, 1], [1, 0, 1, 0]], dtype=np.int64)
    model = PositiveMPS(n, r=2)
    model.fit(X, epochs=100, lr=0.2, verbose=False)

    # all 16 configs, ordered by model probability
    configs = np.array(
        [[(k >> (n - 1 - i)) & 1 for i in range(n)] for k in range(2 ** n)],
        dtype=np.int64,
    )
    probs = np.array([model._log_proba(c) for c in configs])
    order = probs.argsort()[::-1]

    for x in X:
        rank = int(np.where((configs[order] == x).all(axis=1))[0][0])
        # each training mode must be in the top half of all configs
        assert rank < 8, f"training mode {x} ranked only {rank}/16"


if __name__ == "__main__":
    test_learns_prior()
    test_exact_marginals_small()
    print("MPS smoke: OK")

#!/usr/bin/env python3
"""Rasch (1PL) and 2PL fits by maximum likelihood, plus the small amount of
statistics machinery the analysis needs. numpy only -- scipy is not installed
in this environment and the pieces required here are short enough to write.

Both models are fitted to a LONG list of observations, not a matrix, which is
how the incomplete design is handled: a cell that was never run simply does not
appear in the list. This is the property that made the incomplete block design
affordable in the first place -- IRT never needed the rectangle.

  1PL  logit P(correct) = theta_m - b_t
  2PL  logit P(correct) = a_t * (theta_m - b_t),  a_t = exp(alpha_t) > 0

Identification. Adding a constant to every theta and every b leaves every
prediction unchanged, so the likelihood has one flat direction; the reported
scale sets mean(b) = 0. For 2PL there is a second flat direction (rescaling all
a against the spread of theta), fixed by mean(alpha) = 0. Standard errors come
from the pseudo-inverse of the observed information, which is the covariance in
the subspace orthogonal to those flat directions; differences between two
thetas are invariant to the choice and are what the separation test uses.
"""
import math

import numpy as np


def sigmoid(z):
    return 0.5 * (1.0 + np.tanh(0.5 * z))


# ---------------------------------------------------------------------------
# chi-square upper tail, for the likelihood-ratio test (no scipy available)
# ---------------------------------------------------------------------------

def _gser(a, x, itmax=500, eps=3e-12):
    """Regularized lower incomplete gamma P(a,x) by series expansion."""
    if x <= 0:
        return 0.0
    ap, s, d = a, 1.0 / a, 1.0 / a
    for _ in range(itmax):
        ap += 1
        d *= x / ap
        s += d
        if abs(d) < abs(s) * eps:
            break
    return s * math.exp(-x + a * math.log(x) - math.lgamma(a))


def _gcf(a, x, itmax=500, eps=3e-12):
    """Regularized upper incomplete gamma Q(a,x) by continued fraction."""
    tiny = 1e-300
    b, c, d = x + 1.0 - a, 1.0 / tiny, 1.0 / (x + 1.0 - a)
    h = d
    for i in range(1, itmax + 1):
        an = -i * (i - a)
        b += 2.0
        d = an * d + b
        if abs(d) < tiny:
            d = tiny
        c = b + an / c
        if abs(c) < tiny:
            c = tiny
        d = 1.0 / d
        delta = d * c
        h *= delta
        if abs(delta - 1.0) < eps:
            break
    return math.exp(-x + a * math.log(x) - math.lgamma(a)) * h


def chi2_sf(x, df):
    """P(X > x) for X ~ chi-square with df degrees of freedom."""
    if df <= 0:
        return 1.0
    if x <= 0:
        return 1.0
    a, xx = df / 2.0, x / 2.0
    return 1.0 - _gser(a, xx) if xx < a + 1.0 else _gcf(a, xx)


def normal_sf(z):
    """P(Z > z) for a standard normal."""
    return 0.5 * math.erfc(z / math.sqrt(2.0))


# ---------------------------------------------------------------------------
# the fits
# ---------------------------------------------------------------------------

class Fit:
    def __init__(self, kind, models, items, theta, b, alpha, loglik, cov,
                 n_obs, n_params):
        self.kind = kind
        self.models = models          # model ids, in theta order
        self.items = items            # item ids, in b order
        self.theta = theta
        self.b = b
        self.alpha = alpha            # None for 1PL
        self.a = None if alpha is None else np.exp(alpha)
        self.loglik = loglik
        self.cov = cov                # covariance over the packed parameters
        self.n_obs = n_obs
        self.n_params = n_params

    @property
    def aic(self):
        return -2 * self.loglik + 2 * self.n_params

    @property
    def bic(self):
        return -2 * self.loglik + self.n_params * math.log(self.n_obs)

    def theta_se(self):
        return np.sqrt(np.maximum(np.diag(self.cov)[:len(self.theta)], 0.0))

    def b_se(self):
        M = len(self.theta)
        return np.sqrt(np.maximum(np.diag(self.cov)[M:M + len(self.b)], 0.0))

    def diff_se(self, i, j):
        """SE of theta_i - theta_j. Invariant to the identification choice,
        which is why the separation test uses it rather than SE(theta)."""
        v = self.cov[i, i] + self.cov[j, j] - 2 * self.cov[i, j]
        return math.sqrt(max(v, 0.0))

    def p(self, mi, ti):
        z = self.theta[mi] - self.b[ti]
        if self.a is not None:
            z = self.a[ti] * z
        return float(sigmoid(z))


# Why the fits are penalized.
#
# Plain joint ML breaks on this data, and it breaks for a reason that is not a
# bug: a model that passes EVERY informative item it was shown has no finite
# maximum-likelihood ability -- the likelihood keeps rising as theta goes to
# infinity. kimi-k2.6 is exactly that case here. Unpenalized, its theta ran to
# +42 logits, the observed information became singular, every standard error
# blew up to ~470, and the separation statistics collapsed to zero.
#
# The standard remedy for extreme scores in Rasch estimation is a penalized
# (equivalently, weakly Bayesian) estimator. A N(0, 3^2) prior on abilities and
# difficulties is very diffuse -- two prior SDs span +-6 logits, i.e. success
# probabilities from 0.0025 to 0.9975 -- so it barely touches any parameter the
# data actually identifies, while keeping extreme ones finite and giving an
# invertible information matrix. The reported standard errors are then posterior
# SDs. The same penalty is applied to both models so the comparison between them
# is like for like.
PRIOR_SD = 3.0
PRIOR_SD_ALPHA = 0.5


def _fit_1pl(mi, ti, y, M, T, prior_sd=PRIOR_SD, iters=200):
    x = np.zeros(M + T)
    lam = 0.0 if prior_sd is None else 1.0 / prior_sd ** 2

    def obj(x, want_hess=True):
        theta, b = x[:M], x[M:]
        z = theta[mi] - b[ti]
        p = sigmoid(z)
        ll = float(np.sum(y * np.log(np.clip(p, 1e-12, 1)) +
                          (1 - y) * np.log(np.clip(1 - p, 1e-12, 1))))
        pen = -0.5 * lam * float(x @ x)
        r = y - p
        g = np.zeros(M + T)
        np.add.at(g, mi, r)
        np.add.at(g, M + ti, -r)
        g -= lam * x
        if not want_hess:
            return ll, pen, g, None
        w = p * (1 - p)
        H = np.zeros((M + T, M + T))
        np.add.at(H, (mi, mi), -w)
        np.add.at(H, (M + ti, M + ti), -w)
        np.add.at(H, (mi, M + ti), w)
        np.add.at(H, (M + ti, mi), w)
        H[np.diag_indices_from(H)] -= lam
        return ll, pen, g, H

    prev = -np.inf
    for _ in range(iters):
        ll, pen, g, H = obj(x)
        cur = ll + pen
        step = -np.linalg.pinv(H, rcond=1e-12) @ g
        t, best = 1.0, cur
        cand = x
        for _ in range(50):
            trial = x + t * step
            tll, tpen, _, _ = obj(trial, want_hess=False)
            if tll + tpen >= cur:
                cand, best = trial, tll + tpen
                break
            t *= 0.5
        x = cand
        if abs(best - prev) < 1e-12:
            break
        prev = best
    ll, pen, g, H = obj(x)
    theta, b = x[:M].copy(), x[M:].copy()
    shift = b.mean()          # report on the scale where mean difficulty = 0
    b -= shift
    theta -= shift
    cov = np.linalg.pinv(-H, rcond=1e-12)
    return theta, b, ll, cov


def _fit_2pl(mi, ti, y, M, T, init=None, prior_sd=PRIOR_SD,
             prior_sd_alpha=PRIOR_SD_ALPHA, iters=300):
    # Start from the 1PL solution with all discriminations at 1. Starting from
    # zero left the optimizer on a plateau where every Newton step was rejected
    # and the "2PL" fit was silently just the null model.
    x = np.zeros(M + 2 * T)
    if init is not None:
        x[:M + T] = init
    lam = 0.0 if prior_sd is None else 1.0 / prior_sd ** 2
    lam_a = 0.0 if prior_sd_alpha is None else 1.0 / prior_sd_alpha ** 2

    def obj(x, want_hess=True):
        theta, b, alpha = x[:M], x[M:M + T], x[M + T:]
        a = np.exp(np.clip(alpha, -3, 3))
        d = theta[mi] - b[ti]
        z = a[ti] * d
        p = sigmoid(z)
        ll = float(np.sum(y * np.log(np.clip(p, 1e-12, 1)) +
                          (1 - y) * np.log(np.clip(1 - p, 1e-12, 1))))
        pen = (-0.5 * lam * float(x[:M + T] @ x[:M + T])
               - 0.5 * lam_a * float(alpha @ alpha))
        r = y - p
        g = np.zeros(M + 2 * T)
        np.add.at(g, mi, r * a[ti])
        np.add.at(g, M + ti, -r * a[ti])
        np.add.at(g, M + T + ti, r * z)      # dz/dalpha = a*(theta-b) = z
        g[:M + T] -= lam * x[:M + T]
        g[M + T:] -= lam_a * alpha
        if not want_hess:
            return ll, pen, g, None
        n = len(x)
        H = np.zeros((n, n))
        h = 1e-4
        for k in range(n):
            xp, xm = x.copy(), x.copy()
            xp[k] += h
            xm[k] -= h
            _, _, gp, _ = obj(xp, want_hess=False)
            _, _, gm, _ = obj(xm, want_hess=False)
            H[:, k] = (gp - gm) / (2 * h)
        return ll, pen, g, 0.5 * (H + H.T)

    prev = -np.inf
    for _ in range(iters):
        ll, pen, g, H = obj(x)
        cur = ll + pen
        try:
            step = -np.linalg.pinv(H, rcond=1e-10) @ g
        except np.linalg.LinAlgError:
            break
        t, cand, best = 1.0, x, cur
        for _ in range(60):
            trial = x + t * step
            trial[M + T:] = np.clip(trial[M + T:], -3, 3)
            tll, tpen, _, _ = obj(trial, want_hess=False)
            if tll + tpen >= cur:
                cand, best = trial, tll + tpen
                break
            t *= 0.5
        if best <= cur + 1e-12:
            # Newton stalled; take a small gradient step instead
            trial = x + 0.05 * g
            trial[M + T:] = np.clip(trial[M + T:], -3, 3)
            tll, tpen, _, _ = obj(trial, want_hess=False)
            if tll + tpen > cur:
                cand, best = trial, tll + tpen
            else:
                break
        x = cand
        if abs(best - prev) < 1e-12:
            break
        prev = best
    ll, pen, g, H = obj(x)
    theta, b, alpha = x[:M].copy(), x[M:M + T].copy(), x[M + T:].copy()
    shift = b.mean()
    b -= shift
    theta -= shift
    ashift = alpha.mean()     # identification: geometric mean discrimination 1
    alpha -= ashift
    theta *= math.exp(ashift)
    b *= math.exp(ashift)
    cov = np.linalg.pinv(-H, rcond=1e-10)
    return theta, b, alpha, ll, cov


def fit(obs, kind="1PL", prior_sd=PRIOR_SD):
    """obs: list of (model_id, item_id, outcome 0/1)."""
    models = sorted({o[0] for o in obs})
    items = sorted({o[1] for o in obs})
    mix = {m: i for i, m in enumerate(models)}
    tix = {t: i for i, t in enumerate(items)}
    mi = np.array([mix[o[0]] for o in obs])
    ti = np.array([tix[o[1]] for o in obs])
    y = np.array([float(o[2]) for o in obs])
    M, T = len(models), len(items)
    theta, b, ll, cov = _fit_1pl(mi, ti, y, M, T, prior_sd=prior_sd)
    if kind == "1PL":
        # free parameters: M abilities + T difficulties, minus the one flat
        # direction removed by fixing mean(b) = 0
        return Fit("1PL", models, items, theta, b, None, ll, cov, len(y),
                   M + T - 1)
    init = np.concatenate([theta, b])
    theta2, b2, alpha, ll2, cov2 = _fit_2pl(mi, ti, y, M, T, init=init,
                                            prior_sd=prior_sd)
    if ll2 < ll:
        # The 2PL family contains the 1PL (all a = 1); if the optimizer landed
        # lower it did not converge, so fall back to the nested solution rather
        # than report an impossible likelihood.
        theta2, b2, alpha = theta, b, np.zeros(T)
        ll2 = ll
        cov2 = np.zeros((M + 2 * T, M + 2 * T))
        cov2[:M + T, :M + T] = cov
    return Fit("2PL", models, items, theta2, b2, alpha, ll2, cov2, len(y),
               M + 2 * T - 2)


def lr_test(fit_small, fit_big):
    stat = 2 * (fit_big.loglik - fit_small.loglik)
    df = fit_big.n_params - fit_small.n_params
    return stat, df, chi2_sf(max(stat, 0.0), df)


def fit_difficulty_only(obs, theta_by_model, prior_sd=2.0):
    """Estimate b for every item with theta held fixed, under a weak N(0, sd^2)
    prior so that an item nobody passed (or everybody passed) still gets a
    finite difficulty. Used only to give the policy simulation a predicted
    probability for every one of the 36 tasks; the reported IRT fit itself is
    plain ML on the informative items."""
    items = sorted({o[1] for o in obs})
    out = {}
    for it in items:
        rows = [(theta_by_model[m], yy) for m, t, yy in obs if t == it]
        b = 0.0
        for _ in range(200):
            g = -1.0 * b / prior_sd ** 2
            h = -1.0 / prior_sd ** 2
            for th, yy in rows:
                p = float(sigmoid(th - b))
                g += -(yy - p)
                h += -p * (1 - p)
            step = -g / h if h != 0 else 0.0
            b += step
            if abs(step) < 1e-10:
                break
        out[it] = b
    return out


def residual_fit_stats(fit_obj, obs):
    """Infit and outfit mean-square per item.

    outfit = mean of squared standardized residuals -- sensitive to lucky
    passes and careless failures far from an item's difficulty.
    infit   = information-weighted version -- sensitive to misfit among the
    models the item actually discriminates between.
    Expected value of both is 1.0 under the model; > ~1.5 means the item's
    responses are noisier than the scale allows, < ~0.5 means suspiciously
    deterministic (usually an item that is simply on/off rather than graded).
    """
    mix = {m: i for i, m in enumerate(fit_obj.models)}
    tix = {t: i for i, t in enumerate(fit_obj.items)}
    acc = {t: [] for t in fit_obj.items}
    for m, t, y in obs:
        p = fit_obj.p(mix[m], tix[t])
        w = p * (1 - p)
        acc[t].append((y - p, w))
    out = {}
    for t, rows in acc.items():
        z2 = [(r * r) / w for r, w in rows if w > 1e-9]
        outfit = sum(z2) / len(z2) if z2 else float("nan")
        num = sum(r * r for r, _ in rows)
        den = sum(w for _, w in rows)
        infit = num / den if den > 1e-9 else float("nan")
        out[t] = (infit, outfit, len(rows))
    return out


def separation_reliability(theta, theta_se):
    """Rasch person-separation reliability and separation index G.

    Observed spread of theta contains both true spread and measurement error.
    Subtract the mean error variance to get the true variance; G is the ratio
    of true SD to root-mean-square error, i.e. how many distinguishable strata
    the panel occupies. G >= 2 (reliability >= 0.8) is the usual bar for
    "this instrument separates these subjects".
    """
    obs_var = float(np.var(theta, ddof=1))
    mse = float(np.mean(np.square(theta_se)))
    true_var = max(obs_var - mse, 0.0)
    rel = true_var / obs_var if obs_var > 0 else 0.0
    g = math.sqrt(true_var / mse) if mse > 0 else float("inf")
    return rel, g, math.sqrt(true_var), math.sqrt(mse)

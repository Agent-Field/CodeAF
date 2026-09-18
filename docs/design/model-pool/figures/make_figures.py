#!/usr/bin/env python3
"""Every figure in pareto-crewing.tex, from the committed CSVs and from
calibrated simulation.

    python3 make_figures.py            # all seven, as .pdf and .png
    python3 make_figures.py f3 f4      # only those

F1, F2 are drawn from real runs (data/*.csv, written by extract.py from a
private ledger of 202 internal runs). F3-F7 are simulation; each carries the
word "simulated" in its caption in the paper and a note in the panel. Nothing
here reads or writes task text.

Only numpy and matplotlib. No seaborn, no styles beyond the ones set below.
"""

import csv
import math
import os
import sys

import numpy as np

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
from matplotlib.ticker import FuncFormatter

HERE = os.path.dirname(os.path.abspath(__file__))
DATA = os.path.join(HERE, "data")

# Restrained: one ink colour, one accent, one muted fill. No grid boxes, no
# frames on three sides, no colour that carries meaning it has not earned.
INK = "#1a1a1a"
ACCENT = "#a0342c"
MUTED = "#8a8a8a"
FILL = "#d8d8d8"

plt.rcParams.update({
    "figure.dpi": 140,
    "savefig.dpi": 300,
    "font.family": "serif",
    "font.serif": ["Times New Roman", "DejaVu Serif"],
    "font.size": 8.5,
    "axes.labelsize": 8.5,
    "axes.titlesize": 9,
    "axes.linewidth": 0.6,
    "axes.edgecolor": INK,
    "axes.labelcolor": INK,
    "xtick.color": INK,
    "ytick.color": INK,
    "text.color": INK,
    "xtick.labelsize": 7.5,
    "ytick.labelsize": 7.5,
    "xtick.major.width": 0.6,
    "ytick.major.width": 0.6,
    "xtick.major.size": 2.5,
    "ytick.major.size": 2.5,
    "legend.fontsize": 7.5,
    "legend.frameon": False,
    "lines.linewidth": 1.1,
    "lines.markersize": 3.2,
})


def bare(ax, left=True, bottom=True):
    """Two spines, no box. The design language of the repository."""
    ax.spines["top"].set_visible(False)
    ax.spines["right"].set_visible(False)
    ax.spines["left"].set_visible(left)
    ax.spines["bottom"].set_visible(bottom)


def save(fig, stem):
    for ext in ("pdf", "png"):
        fig.savefig(os.path.join(HERE, "%s.%s" % (stem, ext)),
                    bbox_inches="tight", pad_inches=0.02)
    plt.close(fig)
    print("  wrote %s.pdf and %s.png" % (stem, stem))


def read(name):
    with open(os.path.join(DATA, name)) as fh:
        return list(csv.DictReader(fh))


def short(model):
    """'z-ai/glm-5.3-flash' -> 'glm-5.3-flash'. The vendor is not the point."""
    return model.split("/")[-1] if model else ""


def wilson(k, n, z=1.96):
    """Wilson score interval: the honest one at small n, which is all we have."""
    if n == 0:
        return (0.0, 0.0)
    p = k / n
    d = 1 + z * z / n
    centre = (p + z * z / (2 * n)) / d
    half = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / d
    return (max(0.0, centre - half), min(1.0, centre + half))


def dollars(x, _pos=None):
    return ("$%.2f" % x) if x < 1 else ("$%.0f" % x)


# ----------------------------------------------------------------- F1: bills

def f1():
    """Where the money goes, from the real ledger.

    Left: dollars per run by the arm of the design that ran it.  Right: the
    checker's share of the bill, per checker model, from the 68 runs that
    carry a per-seat split.  The right panel is the evidence for Section 7:
    the seat that reads the work can eat most of the bill, and a picker whose
    cost model is a fixed default token shape cannot see it coming.
    """
    runs = read("runs.csv")
    seats = [r for r in read("seatcost.csv")
             if r["checker"] not in ("-", "") and float(r["usd"]) > 0]

    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(7.0, 2.7),
                                   gridspec_kw={"width_ratios": [1.15, 1.0]})

    # --- left: bill per run by crew arm
    groups = {}
    for r in runs:
        groups.setdefault(r["crew"], []).append(float(r["cost_usd"]))
    groups = {k: v for k, v in groups.items() if len(v) >= 8}
    order = sorted(groups, key=lambda k: np.median(groups[k]))
    rng = np.random.default_rng(11)
    for i, key in enumerate(order):
        v = np.array([max(x, 1e-3) for x in groups[key]])
        ax1.scatter(v, i + rng.uniform(-0.17, 0.17, len(v)), s=5,
                    color=MUTED, alpha=0.55, linewidths=0, zorder=2)
        med = np.median(v)
        ax1.plot([med, med], [i - 0.32, i + 0.32], color=ACCENT, lw=1.6, zorder=3)
        ax1.text(v.max() * 1.25, i, "n=%d  med $%.2f" % (len(v), med),
                 va="center", fontsize=6.6, color=MUTED)
    ax1.set_yticks(range(len(order)))
    ax1.set_yticklabels(order, fontsize=7.5)
    ax1.set_xscale("log")
    ax1.set_xlim(5e-3, 120)
    ax1.set_xlabel("dollars for one run (log scale)")
    ax1.xaxis.set_major_formatter(FuncFormatter(dollars))
    ax1.set_title("bill per run, by arm  (%d runs)" % len(runs), loc="left")
    bare(ax1)

    # --- right: checker share of the bill
    byc = {}
    for r in seats:
        byc.setdefault(short(r["checker"]), []).append(
            float(r["usd_checker"]) / float(r["usd"]))
    order2 = sorted(byc, key=lambda k: np.median(byc[k]))
    for i, key in enumerate(order2):
        v = np.array(byc[key])
        ax2.scatter(v, i + rng.uniform(-0.15, 0.15, len(v)), s=6,
                    color=MUTED, alpha=0.6, linewidths=0, zorder=2)
        ax2.plot([np.median(v)] * 2, [i - 0.3, i + 0.3], color=ACCENT, lw=1.6, zorder=3)
        ax2.text(1.02, i, "n=%d" % len(v), va="center", fontsize=6.6, color=MUTED)
    ax2.axvline(0.5, color=INK, lw=0.5, ls=(0, (3, 3)))
    ax2.text(0.515, -0.42, "half the bill", fontsize=6.6, color=INK)
    ax2.set_yticks(range(len(order2)))
    ax2.set_yticklabels(order2, fontsize=7.5)
    ax2.set_xlim(-0.02, 1.0)
    ax2.set_xlabel("checker's share of the run's bill")
    share = np.array([float(r["usd_checker"]) / float(r["usd"]) for r in seats])
    ax2.set_title("median %.0f%%, and %.0f%% of runs above half"
                  % (100 * np.median(share), 100 * (share > 0.5).mean()), loc="left")
    bare(ax2)

    fig.tight_layout()
    save(fig, "f1-bill")


# ------------------------------------------------- F2: the self-report is not a reward

def f2():
    """Why a run's own account of itself cannot be the reward.

    Left: the chance a run carried at least one major finding, by the rating it
    was given.  The relation is not monotone: rating 1 is nearly free of major
    findings, because rating 1 is mostly a run that produced nothing to be
    wrong about.  Right: the information one binary self-report carries about
    the graded bit, as an effective count rho (Prop. ref{prop:reliab}).
    """
    runs = read("runs.csv")
    cells = read("cells.csv")

    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(7.0, 2.6),
                                   gridspec_kw={"width_ratios": [1.2, 1.0]})

    ratings = sorted({int(r["rating"]) for r in runs})
    frac, lo, hi, ns = [], [], [], []
    for g in ratings:
        s = [r for r in runs if int(r["rating"]) == g]
        k = sum(1 for r in s if int(r["major"]) > 0)
        frac.append(k / len(s))
        a, b = wilson(k, len(s))
        lo.append(frac[-1] - a)
        hi.append(b - frac[-1])
        ns.append(len(s))
    x = np.arange(len(ratings))
    ax1.bar(x, frac, width=0.56, color=FILL, edgecolor=INK, linewidth=0.5, zorder=2)
    ax1.errorbar(x, frac, yerr=[lo, hi], fmt="none", ecolor=INK, elinewidth=0.7,
                 capsize=2.2, capthick=0.7, zorder=3)

    # the share of runs at each rating that did no work at all (cell ledger)
    nw = []
    for g in ratings:
        s = [c for c in cells if int(c["rating"]) == g]
        nw.append(sum(1 for c in s if c["nowork"] == "1") / len(s) if s else np.nan)
    ax1.plot(x, nw, color=ACCENT, marker="o", lw=1.0, zorder=4,
             label="share that produced nothing at all")
    for i, n in enumerate(ns):
        ax1.text(i, -0.075, "n=%d" % n, ha="center", fontsize=6.6, color=MUTED)
    ax1.set_xticks(x)
    ax1.set_xticklabels([str(g) for g in ratings])
    ax1.set_xlabel("rating the run was given (1 worst, 5 best)")
    ax1.set_ylabel("share of runs")
    ax1.set_ylim(-0.11, 1.02)
    ax1.set_title("chance of a major finding, by rating", loc="left")
    ax1.legend(loc="upper center", bbox_to_anchor=(0.62, 1.0))
    bare(ax1)

    # --- right: reliability of three candidate proxies for the graded bit
    def reliability(flag):
        tp = fn = fp = tn = 0
        for r in runs:
            z = flag(r)
            if z is None:
                continue
            good = int(r["major"]) == 0
            if good and z:
                tp += 1
            elif good:
                fn += 1
            elif z:
                fp += 1
            else:
                tn += 1
        n = tp + fn + fp + tn
        sens = tp / max(1, tp + fn)
        spec = tn / max(1, fp + tn)
        gamma = sens + spec - 1
        p = (tp + fn) / n
        q = (tp + fp) / n
        rho = gamma * gamma * p * (1 - p) / max(1e-9, q * (1 - q))
        return gamma, min(rho, 1.0), n

    probes = [
        ("the run's own\n\"settled\" claim", lambda r: r["stop"] == "settled"),
        ("the process\nexit code is 0", lambda r: None if r["exit"] == "" else r["exit"] == "0"),
        ("an off-crew\nrating of 4 or 5", lambda r: int(r["rating"]) >= 4),
    ]
    names, gam, rho = [], [], []
    for name, fn_ in probes:
        g, rr, n = reliability(fn_)
        names.append(name)
        gam.append(g)
        rho.append(rr)
    y = np.arange(len(names))
    ax2.barh(y - 0.19, gam, height=0.34, color=FILL, edgecolor=INK,
             linewidth=0.5, label=r"$\gamma=a+b-1$")
    ax2.barh(y + 0.19, rho, height=0.34, color=ACCENT, edgecolor=ACCENT,
             linewidth=0, alpha=0.85, label=r"effective count $\rho$")
    for i in range(len(names)):
        ax2.text(max(gam[i], rho[i]) + 0.02, y[i],
                 r"$\gamma$=%.2f  $\rho$=%.2f" % (gam[i], rho[i]),
                 va="center", fontsize=6.6, color=MUTED)
    ax2.set_yticks(y)
    ax2.set_yticklabels(names, fontsize=7.2)
    ax2.set_xlim(0, 1.15)
    ax2.set_xlabel("relative to one graded observation")
    ax2.set_title("what a proxy is worth", loc="left")
    ax2.legend(loc="lower right", bbox_to_anchor=(1.0, -0.04))
    ax2.invert_yaxis()
    bare(ax2)

    fig.tight_layout()
    save(fig, "f2-selfreport")


# ------------------------------------------------------- F3: hull and the L sweep

def synthetic_front(seed=5, n_models=9, n_seats=2):
    """A small simulated catalog, and every crew that can be built from it.

    Prices are drawn log-uniform over the span the real ledger shows (a little
    over two orders of magnitude between the cheapest and dearest run).  Each
    (seat, model) carries a logit effect that rises with log price with
    diminishing returns plus idiosyncratic noise, and a crew's acceptance is
    the additive-logit rule of Assumption~1.  With two seats and nine models
    there are 81 crews, which is the scale at which the front has both
    supported and unsupported points.
    """
    rng = np.random.default_rng(seed)
    bill = np.exp(rng.uniform(math.log(0.02), math.log(3.0), (n_seats, n_models)))
    theta = (0.45 * np.log(bill / 0.02) / (1 + 0.28 * np.log(bill / 0.02))
             + rng.normal(0, 0.28, bill.shape) - 0.9)
    crews, B, A = [], [], []
    for i in range(n_models):
        for j in range(n_models):
            logit = 0.35 + theta[0, i] + theta[1, j]
            crews.append((i, j))
            B.append(bill[0, i] + bill[1, j])
            A.append(1.0 / (1.0 + math.exp(-logit)))
    return np.array(B), np.array(A)


def pareto_set(bill, acc):
    order = np.argsort(bill)
    keep, best = [], -np.inf
    for i in order:
        if acc[i] > best + 1e-12:
            keep.append(i)
            best = acc[i]
    return np.array(keep)


def lower_hull(bill, fail):
    """Vertices of the lower-left convex hull of (bill, failure probability)."""
    order = np.argsort(bill)
    h = []
    for i in order:
        while len(h) >= 2:
            x1, y1 = bill[h[-2]], fail[h[-2]]
            x2, y2 = bill[h[-1]], fail[h[-1]]
            if (y2 - y1) * (bill[i] - x1) >= (fail[i] - y1) * (x2 - x1):
                h.pop()
            else:
                break
        h.append(i)
    return h


def knee_index(bill, qual):
    """eq (knee): farthest above the chord of the front in (ln bill, quality)."""
    o = np.argsort(bill)
    lb, q = np.log(bill[o]), qual[o]
    slope = (q[-1] - q[0]) / (lb[-1] - lb[0])
    gap = q - (q[0] + slope * (lb - lb[0]))
    return o[int(np.argmax(gap))], gap, o


def f3():
    bill, acc = synthetic_front()
    P = pareto_set(bill, acc)
    b, a = bill[P], acc[P]
    f = 1 - a
    hull = lower_hull(b, f)
    hull_sorted = sorted(hull, key=lambda i: b[i])
    k, gap, o = knee_index(b, a)

    # The breakpoints of the sweep: between consecutive hull vertices the
    # indifferent stake is the dollars the upgrade costs per point of failure
    # probability it removes.
    breaks = []
    for u, v in zip(hull_sorted[:-1], hull_sorted[1:]):
        breaks.append((b[v] - b[u]) / (f[u] - f[v]))

    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(7.0, 3.0))

    unsupported = [i for i in range(len(b)) if i not in hull]
    ax1.scatter(bill, acc, s=9, color=FILL, edgecolor=MUTED, linewidths=0.4,
                zorder=2, label="every crew")
    ax1.scatter(b[unsupported], a[unsupported], s=22, facecolor="none",
                edgecolor=INK, linewidths=0.8, zorder=4,
                label="on the front, chosen by no $L$")
    ax1.plot(b[hull_sorted], a[hull_sorted], color=ACCENT, lw=1.2, zorder=3)
    ax1.scatter(b[hull_sorted], a[hull_sorted], s=20, color=ACCENT, zorder=5,
                label=r"the sweep $L\mapsto a^\star(L)$")
    for j, (u, v) in enumerate(zip(hull_sorted[:-1], hull_sorted[1:])):
        xm = math.sqrt(b[u] * b[v])
        ym = (a[u] + a[v]) / 2
        if b[v] / b[u] < 1.35:          # too close to letter without collision
            continue
        above = j % 2 == 0
        ax1.annotate("$L{=}\\$%.2f$" % breaks[j], (xm, ym),
                     textcoords="offset points",
                     xytext=(0, 7 if above else -12), fontsize=6.2,
                     color=ACCENT, ha="center")
    ax1.scatter([b[k]], [a[k]], s=52, facecolor="none", edgecolor=INK,
                linewidths=1.2, marker="s", zorder=6, label="the balanced knee")
    ax1.set_xscale("log")
    ax1.set_xlabel("expected bill for one task (log scale)")
    ax1.set_ylabel(r"$P(g=1)$")
    ax1.xaxis.set_major_formatter(FuncFormatter(dollars))
    ax1.set_title("simulated: the front, its lower hull, and the sweep", loc="left")
    ax1.legend(loc="lower right", fontsize=6.8)
    bare(ax1)

    # --- right: the stake the knee implies, and what a uniform price cut does
    scales = np.exp(np.linspace(math.log(0.25), math.log(4.0), 60))
    knee_bill, chosen_bill = [], []
    L_user = 5.0
    for s in scales:
        bb = b * s
        kk, _, _ = knee_index(bb, a)
        knee_bill.append(bb[kk] / s)          # read back in unscaled dollars
        J = bb + L_user * f
        chosen_bill.append(bb[int(np.argmin(J))] / s)
    ax2.plot(scales, knee_bill, color=INK, lw=1.2, label="the knee")
    ax2.plot(scales, chosen_bill, color=ACCENT, lw=1.2,
             label=r"$\arg\min_a J(a;L{=}\$5)$")
    ax2.set_xscale("log")
    ax2.set_yscale("log")
    ax2.set_xlabel(r"every price in the catalog multiplied by $s$")
    ax2.set_ylabel("bill of the crew picked\n(in pre-scaling dollars)")
    ax2.set_xticks([0.25, 0.5, 1.0, 2.0, 4.0])
    ax2.set_xticklabels(["0.25", "0.5", "1", "2", "4"])
    ax2.set_yticks([0.1, 0.2, 0.4, 1.0, 2.0])
    ax2.yaxis.set_major_formatter(FuncFormatter(dollars))
    ax2.minorticks_off()
    ax2.set_title("scale invariance is the defect", loc="left")
    ax2.legend(loc="upper right")
    bare(ax2)

    fig.tight_layout()
    save(fig, "f3-hull")

    kk_on_hull = k in hull
    print("    front %d crews, hull %d vertices, unsupported %d; knee on hull: %s"
          % (len(b), len(hull), len(unsupported), kk_on_hull))
    print("    sweep breakpoints (dollars): %s"
          % ", ".join("%.3f" % x for x in breaks))


# ------------------------------------------------------------- F4: cascades

def f4():
    """Break-even for running the cheap band first.

    Escalating is worth it when the cheap band clears the bar often enough to
    pay for itself: p_b > c_b / (c_b' + (1-p_b')L).  With L = 0 that is the
    bare cost ratio; every dollar of stake lowers the bar, because the cascade
    strictly lowers the chance the task ends unaccepted.
    """
    ratio = np.exp(np.linspace(math.log(1.0), math.log(60.0), 400))
    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(7.0, 2.7))

    pb_hi = 0.90
    for L_over_cb, style in [(0.0, "-"), (2.0, (0, (5, 2))), (10.0, (0, (2, 2))),
                             (50.0, (0, (1, 1.6)))]:
        thr = 1.0 / (ratio + (1 - pb_hi) * L_over_cb)
        ax1.plot(ratio, np.clip(thr, 0, 1), ls=style,
                 color=INK if L_over_cb else ACCENT, lw=1.1,
                 label=r"$L/c_b=%g$" % L_over_cb)
    ax1.set_xscale("log")
    ax1.set_xlabel(r"cost ratio $c_{b'}/c_b$ (log scale)")
    ax1.set_ylabel(r"break-even $p_b$")
    ax1.set_ylim(0, 1.0)
    ax1.xaxis.set_major_formatter(FuncFormatter(lambda v, p: "%g" % v))
    ax1.set_title(r"cascade beats going straight to $b'$ above the curve"
                  "\n" r"($p_{b'}=0.90$)", loc="left")
    ax1.legend(loc="upper right")
    bare(ax1)

    # second condition: is the escalation worth buying at all?
    L = np.exp(np.linspace(math.log(0.05), math.log(200), 400))
    for pbp, style in [(0.6, (0, (2, 2))), (0.8, (0, (5, 2))), (0.95, "-")]:
        ax2.plot(L, pbp * L, ls=style, color=INK, lw=1.1,
                 label=r"$p_{b'}=%.2f$" % pbp)
    ax2.axhline(1.0, color=ACCENT, lw=1.0)
    ax2.text(0.06, 1.15, r"$c_{b'}=\$1$", fontsize=6.8, color=ACCENT)
    ax2.set_xscale("log")
    ax2.set_yscale("log")
    ax2.set_xlabel(r"stake $L$ (dollars, log scale)")
    ax2.set_ylabel(r"$p_{b'}L$, the value of escalating")
    ax2.xaxis.set_major_formatter(FuncFormatter(dollars))
    ax2.yaxis.set_major_formatter(FuncFormatter(dollars))
    ax2.set_title(r"escalate at all only when $p_{b'}L > c_{b'}$", loc="left")
    ax2.legend(loc="lower right")
    bare(ax2)

    fig.tight_layout()
    save(fig, "f4-cascade")


# ------------------------------------------------- F5: how many runs to separate

def f5():
    """Runs at one seat to tell two models apart by an effect of Delta.

    N >= 2 z^2 sigma^2 / Delta^2 - kappa, with sigma^2 the Bernoulli variance
    of the grade.  The subtraction is the prior's contribution and it is only
    a saving if the prior is centred right; a prior wrong by more than Delta
    costs runs instead.
    """
    delta = np.linspace(0.03, 0.40, 400)
    z = 1.96
    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(7.0, 2.7))

    for var, style in [(0.25, "-"), (0.1875, (0, (5, 2))), (0.0736, (0, (2, 2)))]:
        n = 2 * z * z * var / delta ** 2
        ax1.plot(delta, n, ls=style, color=INK, lw=1.1,
                 label=r"$\bar p(1-\bar p)=%.3f$" % var)
    ax1.set_yscale("log")
    ax1.set_xlabel(r"effect $\Delta$ on $P(g=1)$")
    ax1.set_ylabel("graded runs per model, at one seat")
    ax1.set_title(r"before the prior ($\kappa=0$), $z=1.96$", loc="left")
    ax1.legend(loc="upper right")
    bare(ax1)

    var = 0.1875
    for kappa, style in [(10, (0, (2, 2))), (30, "-"), (100, (0, (5, 2)))]:
        n = np.clip(2 * z * z * var / delta ** 2 - kappa, 0, None)
        ax2.plot(delta, n, ls=style, color=INK if kappa != 30 else ACCENT,
                 lw=1.2, label=r"$\kappa=%d$" % kappa)
    for d in (0.10, 0.20, 0.30):
        ax2.axvline(d, color=MUTED, lw=0.4, ls=(0, (1, 2)))
        n30 = max(0.0, 2 * z * z * var / d ** 2 - 30)
        ax2.annotate("%.0f" % n30, (d, max(n30, 1.2)), fontsize=6.6,
                     color=ACCENT, ha="left", va="bottom")
    ax2.set_yscale("symlog", linthresh=1.0)
    ax2.set_ylim(0, 500)
    ax2.set_xlabel(r"effect $\Delta$ on $P(g=1)$")
    ax2.set_ylabel("graded runs still needed")
    ax2.set_title(r"with a catalog prior worth $\kappa$ runs "
                  r"($\bar p=0.75$)", loc="left")
    ax2.legend(loc="upper right")
    bare(ax2)

    fig.tight_layout()
    save(fig, "f5-samples")

    for d in (0.1, 0.2, 0.3):
        print("    Delta=%.1f: kappa=0 -> %.0f, kappa=30 -> %.0f, kappa=100 -> %.0f"
              % (d, 2 * z * z * var / d ** 2,
                 max(0.0, 2 * z * z * var / d ** 2 - 30),
                 max(0.0, 2 * z * z * var / d ** 2 - 100)))


# --------------------------------------------------- F6: regret under three rules

def simulate_regret(T=500, reps=80, seed=17, n_models=10, n_seats=2,
                    kappa=6.3, L=5.0, prior_sd=0.18, cap=None, day=50,
                    cold=False):
    """A bandit run calibrated to the ledger's cost spread.

    Truth: quality theta_{s,m} in [0,1] per (seat, model); a crew's acceptance
    is the additive rule of Assumption~\ref{ass:add} on the probability scale,
    p(a) = sum_s nu_s theta_{s,a(s)}, and the grade is Bernoulli(p).  Bills per
    (seat, model) are log-uniform over two orders of magnitude, which is the
    span the real ledger shows, and a dearer model is better on average but not
    reliably so.

    Estimation is the exact Gaussian linear model: the feature of a crew is the
    indicator of its (seat, model) pairs weighted by nu, so ONE grade updates
    every seat on the crew -- the semi-bandit structure of Section 8 -- and the
    posterior is maintained by Sherman-Morrison.  The prior is the catalog: the
    truth plus N(0, prior_sd^2), held at strength kappa, which is exactly the
    N/(N+kappa) blend the picker ships.

    Three rules, differing ONLY in how the crew is chosen from that posterior:
    the posterior mean (today's rule, which never explores), a Thompson draw,
    and a Thompson draw with a hard per-day cap on exploration spend.
    """
    rng = np.random.default_rng(seed)
    nu = 1.0 / n_seats
    d = n_seats * n_models
    sig_eps = 0.45                     # sd of one Bernoulli grade, near p=0.7
    sig0 = sig_eps / math.sqrt(kappa)  # the prior's strength, as a variance
    rules = ("blend", "ts", "ts_cap")
    regret = {k: np.zeros(T) for k in rules}
    spend = {k: np.zeros(T) for k in rules}

    def idx(s, m):
        return s * n_models + m

    for _ in range(reps):
        bill = np.exp(rng.uniform(math.log(0.02), math.log(2.0), (n_seats, n_models)))
        theta = np.clip(0.45 + 0.13 * np.log(bill / bill.mean())
                        + rng.normal(0, 0.12, bill.shape), 0.05, 0.95)
        prior = np.clip(theta + rng.normal(0, prior_sd, theta.shape), 0.02, 0.98)
        # A model the catalog does not index at all: the prior is the seat's
        # average and carries no information about THIS model.  Make it the
        # best one, which is the case the greedy rule cannot recover from.
        cold_sd = np.full(theta.shape, sig0)
        if cold:
            for s_ in range(n_seats):
                theta[s_, 0] = min(0.95, theta[s_].max() + 0.10)
                prior[s_, 0] = float(prior[s_, 1:].mean())
                cold_sd[s_, 0] = sig0 * 3.0

        crew_bill = bill[0][:, None] + bill[1][None, :]  # after any cold edit
        crew_p = np.clip(nu * (theta[0][:, None] + theta[1][None, :]), 0.02, 0.98)
        J_true = crew_bill + L * (1 - crew_p)
        best = J_true.min()

        state = {}
        for k in rules:
            state[k] = [prior.reshape(-1).copy(),                 # posterior mean
                        np.diag(cold_sd.reshape(-1) ** 2)]        # posterior cov
        used = {k: 0.0 for k in rules}

        for t in range(T):
            if t % day == 0:
                for k in rules:
                    used[k] = 0.0
            for k in rules:
                mu, Sigma = state[k]
                m = mu.reshape(n_seats, n_models)
                if k == "blend":
                    draw = m
                else:
                    # A draw from N(mu, Sigma); the Cholesky is of a 2n x 2n
                    # matrix, so this is cheap at the scale that matters.
                    chol = np.linalg.cholesky(Sigma + 1e-12 * np.eye(d))
                    draw = (mu + chol @ rng.standard_normal(d)).reshape(n_seats, n_models)

                def pick_from(q):
                    ph = np.clip(nu * (q[0][:, None] + q[1][None, :]), 0.0, 1.0)
                    J = crew_bill + L * (1 - ph)
                    return np.unravel_index(int(np.argmin(J)), J.shape)

                pick = pick_from(draw)
                greedy = pick_from(m)
                extra = max(0.0, crew_bill[pick] - crew_bill[greedy])
                if k == "ts_cap" and cap is not None and used[k] + extra > cap:
                    pick, extra = greedy, 0.0
                used[k] += extra

                regret[k][t] += J_true[pick] - best
                spend[k][t] += extra

                g = 1.0 if rng.random() < crew_p[pick] else 0.0
                x = np.zeros(d)
                x[idx(0, pick[0])] = nu
                x[idx(1, pick[1])] = nu
                # Sherman-Morrison on the covariance, then the usual Gaussian
                # posterior mean update. y is the grade; its mean is x'theta.
                Sx = Sigma @ x
                denom = sig_eps ** 2 + x @ Sx
                Sigma = Sigma - np.outer(Sx, Sx) / denom
                mu = mu + Sx * (g - x @ mu) / denom
                state[k] = [mu, Sigma]

    for k in rules:
        regret[k] /= reps
        spend[k] /= reps
    return regret, spend


def f6():
    """Does exploration pay, and when.

    Left: every model carries a catalog index, so the prior is informative
    everywhere.  The posterior-mean rule wins: a Thompson draw buys evidence
    the horizon is too short to use, and the cap recovers most of the
    difference.  Middle: one model per seat carries NO catalog index and is in
    fact the best at its seat.  The posterior-mean rule never plays it and its
    regret does not fall; the Thompson rule finds it.  Right: what exploration
    costs, with and without the per-day cap.
    """
    T, reps, cap = 800, 60, 0.60
    warm, warm_spend = simulate_regret(T=T, reps=reps, cap=cap, cold=False)
    cold, cold_spend = simulate_regret(T=T, reps=reps, cap=cap, cold=True)
    t = np.arange(1, T + 1)

    labels = {"blend": "posterior mean (shipped)",
              "ts": r"Thompson inside $J$",
              "ts_cap": r"Thompson, capped"}
    styles = {"blend": ((0, (2, 2)), INK), "ts": ("-", ACCENT),
              "ts_cap": ((0, (5, 2)), INK)}

    fig, axes = plt.subplots(1, 3, figsize=(7.1, 2.5))
    for ax, run, title in ((axes[0], warm, "every model indexed"),
                           (axes[1], cold, "one unindexed model per seat,\nand it is the best one")):
        for k in ("blend", "ts", "ts_cap"):
            style, col = styles[k]
            ax.plot(t, np.cumsum(run[k]), ls=style, color=col, lw=1.2, label=labels[k])
        ax.set_xlabel("tasks")
        ax.set_title(title, loc="left")
        bare(ax)
    axes[0].set_ylabel("cumulative regret (dollars)")
    axes[0].legend(loc="upper left")

    ax = axes[2]
    for run, lbl, col in ((warm_spend, "indexed", INK), (cold_spend, "unindexed", ACCENT)):
        ax.plot(t, np.cumsum(run["ts"]), ls="-", color=col, lw=1.2,
                label="%s, uncapped" % lbl)
        ax.plot(t, np.cumsum(run["ts_cap"]), ls=(0, (5, 2)), color=col, lw=1.0,
                label="%s, capped" % lbl)
    ax.set_xlabel("tasks")
    ax.set_ylabel("cumulative exploration spend (dollars)")
    ax.set_title(r"the cap is $E=\$%.2f$ per 50 tasks" % cap, loc="left")
    ax.legend(loc="upper left")
    bare(ax)

    fig.tight_layout()
    save(fig, "f6-regret")
    for name, run in (("indexed", warm), ("unindexed", cold)):
        print("    %-10s " % name + "  ".join(
            "%s $%.1f (last100 $%.3f/task)" % (k, np.cumsum(run[k])[-1], run[k][-100:].mean())
            for k in ("blend", "ts", "ts_cap")))


# ------------------------------------------------------------------ F7: drift

def f7():
    """An arm nobody runs gets its uncertainty back.

    Variance inflation V <- V + tau^2 dt between observations, capped at the
    prior; a re-release under the same identifier is a change point and resets
    both the mean's weight and the variance to the prior.
    """
    days = np.arange(0, 91)
    sigma0 = 0.55
    obs_days = set(range(0, 21))          # run daily for three weeks, then never
    reset_day = 60
    fig, ax = plt.subplots(figsize=(4.6, 2.6))

    for tau, style in [(0.010, (0, (2, 2))), (0.030, "-"), (0.060, (0, (5, 2)))]:
        v = sigma0 ** 2
        sd = []
        for d in days:
            if d == reset_day:
                v = sigma0 ** 2
            else:
                v = min(sigma0 ** 2, v + tau ** 2)
            if d in obs_days:
                v = 1.0 / (1.0 / v + 1.0 / (0.9 ** 2))
            sd.append(math.sqrt(v))
        ax.plot(days, sd, ls=style, color=ACCENT if tau == 0.030 else INK,
                lw=1.1, label=r"$\tau=%.3f$/day" % tau)

    ax.axvspan(0, 20, color=FILL, alpha=0.55, lw=0)
    ax.text(10, sigma0 * 1.02, "run daily", ha="center", fontsize=6.8, color=MUTED)
    ax.axvline(reset_day, color=INK, lw=0.6)
    ax.text(reset_day + 1.5, sigma0 * 0.55, "re-released\nunder the same id",
            fontsize=6.8, color=INK)
    ax.axhline(sigma0, color=MUTED, lw=0.5, ls=(0, (1, 2)))
    ax.text(88, sigma0 * 1.02, "prior", ha="right", fontsize=6.8, color=MUTED)
    ax.set_xlabel("days")
    ax.set_ylabel(r"posterior sd of $\theta_{s,m}$ (logit)")
    ax.set_ylim(0, sigma0 * 1.18)
    ax.set_title("simulated", loc="left")
    ax.legend(loc="lower left", bbox_to_anchor=(0.02, 0.02))
    bare(ax)

    fig.tight_layout()
    save(fig, "f7-drift")


FIGURES = {"f1": f1, "f2": f2, "f3": f3, "f4": f4, "f5": f5, "f6": f6, "f7": f7}


def main():
    want = [a.lower() for a in sys.argv[1:]] or list(FIGURES)
    for key in want:
        if key not in FIGURES:
            raise SystemExit("no such figure: %s (have %s)" % (key, ", ".join(FIGURES)))
        print(key)
        FIGURES[key]()


if __name__ == "__main__":
    main()

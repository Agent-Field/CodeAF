#!/usr/bin/env python3
"""Draws the figures in pareto-crewing.tex from data/*.csv. Standard library, numpy and matplotlib only.

    python3 figures.py            # writes fig-*.pdf and tab-*.tex beside this file

The simulations are the ones the paper describes. Ground truth is the table of measured crews fitted
from our own scored runs (data/measured-crews.csv). A learner picks a crew per run, sees the run's
outcome and its dollars, and pays the all-in cost E = c + lambda*t + H*(1 - clean). Regret is E minus
the oracle crew's, averaged over replicates. Every replicate draws its own noisy catalog prior.
"""
import csv, os, random
import numpy as np
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt

HERE = os.path.dirname(os.path.abspath(__file__))
H, LAM, K = 10.0, 0.01, 30
REPS = 200
plt.rcParams.update({"font.size": 9, "font.family": "serif", "axes.spines.top": False, "axes.spines.right": False})


def crews():
    rows = list(csv.DictReader(open(os.path.join(HERE, "data", "measured-crews.csv"))))
    for r in rows:
        for k in ("delivered", "clean", "usd", "minutes"):
            r[k] = float(r[k])
        r["runs"] = int(r["runs"])
        r["name"] = r["worker"] + ("+" + r["checker"] if r["checker"] != "-" else "") + ("/" + r["planner"] if r["planner"] != "-" else "")
        r["run"] = r["usd"] + LAM * r["minutes"]
        r["p"] = r["delivered"] * r["clean"]
    return rows


def allin(r, h=H):
    return r["run"] + h * (1 - r["p"])


def fig_front():
    rows = crews()
    fig, ax = plt.subplots(figsize=(5.4, 3.5))
    for door, mark, label in (("task", "o", "chat door (worker + checker)"), ("do", "s", "headless door (worker + planner)")):
        pts = [r for r in rows if r["door"] == door]
        ax.scatter([r["run"] for r in pts], [100 * r["p"] for r in pts], s=18, marker=mark, alpha=.75, label=label)
    front, best = [], -1
    for r in sorted(rows, key=lambda r: r["run"]):
        if r["p"] > best:
            front.append(r)
            best = r["p"]
    ax.plot([r["run"] for r in front], [100 * r["p"] for r in front], "k-", lw=.8, label="pareto front")
    for r in front:
        ax.annotate(r["name"], (r["run"], 100 * r["p"]), fontsize=6, xytext=(4, -9), textcoords="offset points")
    xs = np.linspace(0.05, 4, 100)
    for h, e, x in ((1, 1.2, 1.0), (10, 3.0, 2.4), (50, 12.0, 3.2)):
        ax.plot(xs, 100 * (1 - (e - xs) / h), color="#999", lw=.6, ls="--")
        ax.text(x, 100 * (1 - (e - x) / h) + 1.5, f"E = \\${e:.0f} at H = \\${h}", fontsize=6, color="#777")
    ax.set_xlim(0, 4)
    ax.set_ylim(0, 100)
    ax.set_xlabel("cost of one run, $ (dollars + λ · minutes)")
    ax.set_ylabel("P(delivered and clean), %")
    ax.legend(fontsize=7, loc="lower left")
    fig.tight_layout()
    fig.savefig(os.path.join(HERE, "fig-front.pdf"))


def fig_cells():
    rows = list(csv.DictReader(open(os.path.join(HERE, "data", "seed-cells.csv"))))
    fig, axes = plt.subplots(1, 3, figsize=(6.2, 2.6))
    for ax, role in zip(axes, ("worker", "high", "mastermind")):
        cells = sorted([r for r in rows if r["role"] == role], key=lambda r: -float(r["mean"]))
        names = [r["model"].split("/")[-1] for r in cells]
        ax.barh(names, [float(r["mean"]) for r in cells], xerr=[float(r["sd"]) for r in cells], color="#8a8fb0", ecolor="#333", capsize=2)
        for i, r in enumerate(cells):
            ax.text(2, i, f"n={r['n']}", va="center", fontsize=6, color="white")
        ax.set_title(role, fontsize=9)
        ax.set_xlim(0, 100)
        ax.invert_yaxis()
        ax.tick_params(axis="y", labelsize=7)
    axes[0].set_xlabel("role_quality, posterior mean ± sd")
    fig.tight_layout()
    fig.savefig(os.path.join(HERE, "fig-cells.pdf"))


def fig_shrink():
    q_cat, s_bar, sigma = 80.0, 65.0, 15.0
    n = np.arange(0, 121)
    mean = (K * q_cat + n * s_bar) / (K + n)
    sd = sigma / np.sqrt(K + n)
    fig, ax = plt.subplots(figsize=(5.2, 2.8))
    ax.fill_between(n, mean - 1.96 * sd, mean + 1.96 * sd, color="#8a8fb0", alpha=.3, label="95% posterior band")
    ax.plot(n, mean, "k-", label="posterior mean")
    ax.axhline(q_cat, ls=":", color="#555", lw=.8)
    ax.axhline(s_bar, ls="--", color="#555", lw=.8)
    ax.text(2, q_cat + 0.8, "catalog prior", fontsize=7)
    ax.text(100, s_bar + 0.8, "measured mean", fontsize=7)
    ax.axvline(K, color="#a55", lw=.8)
    ax.text(K + 1.5, 62, "n = k = 30: half weight each", fontsize=7, color="#a55")
    ax.set_xlabel("effective observations n")
    ax.set_ylabel("role quality")
    ax.set_xlim(0, 120)
    ax.set_ylim(60, 84)
    ax.legend(fontsize=7, loc="upper right")
    fig.tight_layout()
    fig.savefig(os.path.join(HERE, "fig-shrink.pdf"))


def simulate(strategy, N, seed, rows, oracle):
    """Crew-level learners: every crew is one arm with one Beta posterior on P(clean)."""
    rnd = random.Random(seed)
    truth = {r["name"]: r["p"] for r in rows}
    prior = {n: min(.99, max(.01, p + rnd.gauss(0, .15))) for n, p in truth.items()}
    a = {n: 1 + K * prior[n] for n in truth}
    b = {n: 1 + K * (1 - prior[n]) for n in truth}
    fixed = {r["name"]: r["run"] for r in rows}
    total, explored = 0.0, 0
    for t in range(N):
        mean_best = min(truth, key=lambda n: fixed[n] + H * (1 - a[n] / (a[n] + b[n])))
        if strategy == "table":
            pick = rows[1]["name"]
        elif strategy == "catalog":
            pick = min(truth, key=lambda n: fixed[n] + H * (1 - prior[n]))
        elif strategy == "greedy":
            pick = mean_best if rnd.random() >= 0.1 else rnd.choice(list(truth))
        else:
            pick = min(truth, key=lambda n: fixed[n] + H * (1 - rnd.betavariate(a[n], b[n])))
        explored += pick != mean_best
        clean = rnd.random() < truth[pick]
        total += fixed[pick] + H * (1 - clean) - oracle
        if strategy in ("greedy", "thompson"):
            a[pick] += clean
            b[pick] += not clean
    return total / N, explored / N


def fig_regret():
    rows = [r for r in crews() if r["door"] == "task"]
    oracle = min(allin(r) for r in rows)
    Ns = [10, 20, 40, 70, 100, 150, 200, 300, 400]
    fig, (ax, ax2) = plt.subplots(1, 2, figsize=(6.2, 2.8))
    table = {}
    for s, style in (("table", "k:"), ("catalog", "k--"), ("greedy", "C1-"), ("thompson", "C0-")):
        reg, exp = [], []
        for N in Ns:
            rs = [simulate(s, N, seed, rows, oracle) for seed in range(REPS)]
            reg.append(np.mean([r[0] for r in rs]))
            exp.append(np.mean([r[1] for r in rs]))
        table[s] = reg
        ax.plot(Ns, reg, style, label={"table": "fixed table row", "catalog": "catalog only", "greedy": "ε-greedy, ε = 0.1", "thompson": "Thompson"}[s])
        if s in ("greedy", "thompson"):
            ax2.plot(Ns, [100 * e for e in exp], style, label=s)
    ax.set_xlabel("runs N")
    ax.set_ylabel("regret vs oracle, $ per task")
    ax.set_ylim(0, None)
    ax.legend(fontsize=7)
    ax2.set_xlabel("runs N")
    ax2.set_ylabel("runs off the posterior-mean pick, %")
    ax2.set_ylim(0, None)
    ax2.legend(fontsize=7)
    fig.tight_layout()
    fig.savefig(os.path.join(HERE, "fig-regret.pdf"))
    with open(os.path.join(HERE, "tab-regret.tex"), "w") as f:
        f.write("\\begin{tabular}{l" + "r" * len(Ns) + "}\\toprule\n$N$ & " + " & ".join(str(n) for n in Ns) + "\\\\\\midrule\n")
        for s in ("table", "catalog", "greedy", "thompson"):
            f.write(s + " & " + " & ".join(f"{v:.2f}" for v in table[s]) + "\\\\\n")
        f.write("\\bottomrule\\end{tabular}\n")


VENDOR = {"ds-v4.1-flash": "deepseek", "ds-v4-pro-0813": "deepseek", "ds-v4-flash-0731": "deepseek", "glm-5.3-flash": "z-ai",
          "glm-5.3": "z-ai", "muse-spark-1.3": "meta", "qwen3.8-max": "qwen", "kimi-k3": "moonshot", "fable-5.1": "anthropic",
          "opus-5": "anthropic"}


def role_truth():
    """A three-role ground truth read off the measured rows under the linear rule of the paper: a role quality
    q_r(m) in [0, 100] per (role, model) and a dollar surcharge per (role, model); a crew's P(clean) is the mean of
    its role qualities over 100 and its cost the sum of its surcharges. Workers and checkers come from the chat
    rows (delivered is the worker's, clean the checker's), planners from the headless rows."""
    rows = crews()
    chat = [r for r in rows if r["door"] == "task"]
    do = [r for r in rows if r["door"] == "do"]
    mean = lambda xs: float(np.mean(xs))
    q = {"worker": {}, "checker": {}, "planner": {}}
    cost = {"worker": {}, "checker": {}, "planner": {}}
    for w in sorted({r["worker"] for r in chat}):
        mine = [r for r in chat if r["worker"] == w]
        q["worker"][w] = 100 * mean([r["delivered"] for r in mine])
        cost["worker"][w] = mean([r["run"] for r in mine])
    base = mean([r["run"] for r in chat])
    for c in sorted({r["checker"] for r in chat}):
        mine = [r for r in chat if r["checker"] == c]
        q["checker"][c] = 100 * mean([r["clean"] for r in mine])
        cost["checker"][c] = mean([r["run"] for r in mine]) - base
    dobase = mean([r["run"] for r in do])
    for p in sorted({r["planner"] for r in do}):
        mine = [r for r in do if r["planner"] == p]
        q["planner"][p] = 100 * mean([r["clean"] for r in mine])
        cost["planner"][p] = max(0.0, mean([r["run"] for r in mine]) - dobase)
    grid = {}
    for w in q["worker"]:
        for c in q["checker"]:
            if VENDOR.get(w) == VENDOR.get(c):
                continue
            for p in q["planner"]:
                k = (w, c, p)
                grid[k] = dict(p=(q["worker"][w] + q["checker"][c] + q["planner"][p]) / 300,
                               run=cost["worker"][w] + cost["checker"][c] + cost["planner"][p])
    return q, grid


def simulate_roles(level, N, seed, q, grid, sigma=15.0):
    """Thompson sampling under three kinds of feedback on the same truth: one Beta arm per crew fed the run's
    clean/defect bit; one Gaussian arm per crew fed the mean of the judge's role scores; one Gaussian posterior per
    (role, model) fed each role's own judge score (semi-bandit). Judge scores are the role quality plus N(0, sigma)."""
    rnd = random.Random(seed)
    arms = list(grid)
    roles = ("worker", "checker", "planner")
    oracle = min(grid[a]["run"] + H * (1 - grid[a]["p"]) for a in arms)
    noise = lambda x: min(99.0, max(1.0, x + rnd.gauss(0, 15)))
    prior_q = {r: {m: noise(q[r][m]) for m in q[r]} for r in roles}
    prior_p = {a: sum(prior_q[r][a[i]] for i, r in enumerate(roles)) / 300 for a in arms}
    if level == "crew-bit":
        a_ = {a: 1 + K * prior_p[a] for a in arms}
        b_ = {a: 1 + K * (1 - prior_p[a]) for a in arms}
    elif level == "crew-score":
        n_ = {a: float(K) for a in arms}
        s_ = {a: K * 100 * prior_p[a] for a in arms}
    else:
        n_ = {(r, m): float(K) for r in roles for m in q[r]}
        s_ = {(r, m): K * prior_q[r][m] for r in roles for m in q[r]}
    total = 0.0
    for t in range(N):
        if level == "crew-bit":
            pick = min(arms, key=lambda a: grid[a]["run"] + H * (1 - rnd.betavariate(a_[a], b_[a])))
        elif level == "crew-score":
            pick = min(arms, key=lambda a: grid[a]["run"] + H * (1 - rnd.gauss(s_[a] / n_[a], sigma / n_[a] ** .5) / 100))
        else:
            draw = {k: rnd.gauss(s_[k] / n_[k], sigma / n_[k] ** .5) for k in n_}
            pick = min(arms, key=lambda a: grid[a]["run"] + H * (1 - sum(draw[(r, a[i])] for i, r in enumerate(roles)) / 300))
        g = grid[pick]
        clean = rnd.random() < g["p"]
        total += g["run"] + H * (1 - clean) - oracle
        scores = [q[r][pick[i]] + rnd.gauss(0, sigma) for i, r in enumerate(roles)]
        if level == "crew-bit":
            a_[pick] += clean; b_[pick] += not clean
        elif level == "crew-score":
            n_[pick] += 1; s_[pick] += float(np.mean(scores))
        else:
            for i, r in enumerate(roles):
                n_[(r, pick[i])] += 1; s_[(r, pick[i])] += scores[i]
    return total / N


def fig_semibandit():
    q, grid = role_truth()
    Ns = [10, 20, 40, 70, 100, 150, 200, 300, 400]
    fig, ax = plt.subplots(figsize=(4.8, 2.9))
    table = {}
    for level, style, label in (("crew-bit", "C0:", "one arm per crew, clean/defect bit"),
                                ("crew-score", "C0--", "one arm per crew, mean judge score"),
                                ("role", "C2-", "one posterior per (role, model), role scores")):
        reg = [np.mean([simulate_roles(level, N, seed, q, grid) for seed in range(REPS)]) for N in Ns]
        table[level] = reg
        ax.plot(Ns, reg, style, label=label)
    d = sum(len(q[r]) for r in q)
    ax.set_title(f"{len(grid)} crews from {len(q['worker'])} workers × {len(q['checker'])} checkers × {len(q['planner'])} planners; {d} unknowns", fontsize=8)
    ax.set_xlabel("runs N")
    ax.set_ylabel("regret vs oracle, $ per task")
    ax.set_ylim(0, None)
    ax.legend(fontsize=7)
    fig.tight_layout()
    fig.savefig(os.path.join(HERE, "fig-semibandit.pdf"))
    with open(os.path.join(HERE, "tab-semibandit.tex"), "w") as f:
        f.write("\\begin{tabular}{l" + "r" * len(Ns) + "}\\toprule\n$N$ & " + " & ".join(str(n) for n in Ns) + "\\\\\\midrule\n")
        for level, name in (("crew-bit", "crew, clean bit"), ("crew-score", "crew, mean score"), ("role", "role, semi-bandit")):
            f.write(name + " & " + " & ".join(f"{v:.2f}" for v in table[level]) + "\\\\\n")
        f.write("\\bottomrule\\end{tabular}\n")


if __name__ == "__main__":
    fig_front()
    fig_cells()
    fig_shrink()
    fig_regret()
    fig_semibandit()
    print("figures written")

#!/usr/bin/env python3
"""The final board: one aforge, the old build, pi and opencode.

Two panels a person can read in one glance — quality against time and
quality against cost, one dot per (arm, task), the finalist in the accent hue
and everything else as context. A third panel is the size ladder: the same
quality by task, tasks ordered by the size of the ask, so "where we shine"
reads left to right.

Usage: final_board.py [finalist-arm]   (default: aforge-final-crew if present)
"""
import csv, json, os, statistics, sys
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
from matplotlib.lines import Line2D

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
DIMS = ["requirement_coverage", "correctness", "scope_discipline", "completeness", "report_honesty"]
RES = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "results")

SURFACE, INK, INK2, GRID = "#fcfcfb", "#0b0b0b", "#52514e", "#e8e7e4"
ACCENT, OLD, GRAY1, GRAY2 = "#2a78d6", "#1baf7a", "#8a8985", "#b5b4b0"
# Task size ladder: the ask's own width — one small fix, two medium features,
# four issues at once. #20 is a no-op row and is excluded from the board.
LADDER = ["23", "21", "22", "batch"]
LABELS = {"23": "#23 · small", "21": "#21 · medium", "22": "#22 · medium", "batch": "batch · 4 issues"}

def judge_total(cell):
    scores = []
    for j in "abcd":
        p = os.path.join(RES, cell, f"judge_{j}.json")
        if not os.path.exists(p): continue
        try:
            o = json.load(open(p))
            if o.get("weakened_tests"): return 0
            scores.append(sum(int(o[d]) for d in DIMS))
        except Exception: pass
    return statistics.median(scores) if len(scores) >= 3 else None

def load():
    rows = list(csv.DictReader(open(os.path.join(RES, "results.csv"))))
    out = []
    for r in rows:
        if r["task"] not in LADDER or r["outcome"].startswith("KILLED"): continue
        cell = f'{r["harness"]}-{r["task"]}-{r["seed"]}'
        wall = float(r["wall_s"] or 0); active = float(r.get("wall_s_active") or wall)
        tb = int(r["tests_before"].split("/")[0]) if r["tests_before"] else 0
        ta, fa = (int(x) for x in r["tests_after"].split("/")) if r["tests_after"] else (0, 0)
        failed = (not r["outcome"].startswith("OK")) or ta < tb or fa > 0
        q = 0 if failed else judge_total(cell)
        cost = r.get("cost_list_usd") or r["cost_usd"]
        out.append({"arm": r["harness"], "task": r["task"], "active": active if 0 < active <= wall else wall,
                    "cost": float(cost) if cost else None, "q": q, "failed": failed})
    return out

def main():
    data = load()
    arms = {r["arm"] for r in data}
    finalist = sys.argv[1] if len(sys.argv) > 1 else ("aforge-final-crew" if "aforge-final-crew" in arms else "aforge-pre-flash")
    show = {finalist: ("aforge", ACCENT, "o"), "aforge-old-flash": ("aforge (old)", OLD, "o"),
            "pi": ("pi", GRAY1, "s"), "opencode": ("opencode", GRAY2, "^")}
    data = [r for r in data if r["arm"] in show and r["q"] is not None]

    plt.rcParams.update({"figure.facecolor": SURFACE, "axes.facecolor": SURFACE, "text.color": INK,
                         "axes.edgecolor": GRID, "axes.labelcolor": INK2, "xtick.color": INK2, "ytick.color": INK2,
                         "font.size": 10, "axes.grid": True, "grid.color": GRID, "grid.linewidth": 0.6,
                         "axes.axisbelow": True, "font.family": "DejaVu Sans"})
    fig, (ax1, ax2, ax3) = plt.subplots(1, 3, figsize=(15, 5))

    def dot(ax, x, y, arm, failed):
        label, color, marker = show[arm]
        ax.scatter(x, y, s=120 if arm == finalist else 80, c=color, marker=marker, edgecolors=SURFACE, linewidths=2, zorder=3)
        if failed: ax.scatter(x, y, s=280, facecolors="none", edgecolors="#e34948", linewidths=1.6, zorder=4)

    for r in data:
        dot(ax1, r["active"], r["q"], r["arm"], r["failed"])
        if r["cost"] is not None: dot(ax2, r["cost"], r["q"], r["arm"], r["failed"])
    ax1.set_xscale("log"); ax1.set_xlabel("active seconds (log)"); ax1.set_ylabel("quality — blind judge median (0–20)")
    ax1.set_title("quality × time, one dot per task", fontsize=11, pad=8)
    ax2.set_xscale("log"); ax2.set_xlabel("cost, tokens × list price, USD (log)")
    from matplotlib.ticker import FixedLocator, FixedFormatter, NullFormatter
    ticks = [0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1, 2]
    ax2.xaxis.set_major_locator(FixedLocator(ticks)); ax2.xaxis.set_major_formatter(FixedFormatter([f"${t:g}" for t in ticks]))
    ax2.xaxis.set_minor_formatter(NullFormatter())
    ax1.xaxis.set_major_locator(FixedLocator([60, 120, 240, 480, 960, 1920])); ax1.xaxis.set_major_formatter(FixedFormatter(["60", "120", "240", "480", "960", "1920"]))
    ax1.xaxis.set_minor_formatter(NullFormatter())
    ax2.set_title("quality × cost, one dot per task", fontsize=11, pad=8)
    for ax in (ax1, ax2):
        ax.set_ylim(-1, 21)
        for s in ("top", "right"): ax.spines[s].set_visible(False)

    # Size ladder: per arm, a line across the tasks in ask-size order.
    for arm, (label, color, marker) in show.items():
        ys = []
        for t in LADDER:
            v = [r["q"] for r in data if r["arm"] == arm and r["task"] == t]
            ys.append(v[0] if v else float("nan"))
        ax3.plot(range(len(LADDER)), ys, color=color, marker=marker, markersize=8 if arm == finalist else 6,
                 linewidth=2.2 if arm == finalist else 1.4, markeredgecolor=SURFACE, markeredgewidth=1.5, zorder=3 if arm == finalist else 2)
    ax3.set_xticks(range(len(LADDER))); ax3.set_xticklabels([LABELS[t] for t in LADDER], fontsize=9)
    ax3.set_ylim(-1, 21); ax3.set_title("quality by size of the ask", fontsize=11, pad=8)
    for s in ("top", "right"): ax3.spines[s].set_visible(False)

    handles = [Line2D([], [], linestyle="", marker=m, color=c, markersize=9, markeredgecolor=SURFACE, markeredgewidth=1.5, label=l)
               for l, c, m in show.values()]
    handles.append(Line2D([], [], linestyle="", marker="o", markersize=11, markerfacecolor="none", markeredgecolor="#e34948", label="failed: DNF / regressed / weakened test"))
    fig.legend(handles=handles, loc="lower center", ncol=5, frameon=False, fontsize=9, bbox_to_anchor=(0.5, -0.03))
    fig.suptitle("aforge against pi and opencode — same model (deepseek-v4-flash), same real issues, three blind judges per cell",
                 fontsize=12.5, color=INK, y=1.0)
    fig.tight_layout(rect=[0, 0.05, 1, 0.96])
    out = os.path.join(os.path.dirname(os.path.abspath(__file__)), "final_board.png")
    fig.savefig(out, dpi=150, bbox_inches="tight", facecolor=SURFACE); print("wrote", out)

    # The one-line verdict: dominance over the three axes, per arm.
    print(f"\n{'arm':14} {'quality':>8} {'med s':>6} {'cost $':>7}  failed")
    for arm, (label, *_ ) in show.items():
        sub = [r for r in data if r["arm"] == arm]
        if not sub: continue
        c = [r["cost"] for r in sub if r["cost"] is not None]
        print(f"{label:14} {statistics.mean(r['q'] for r in sub):>8.1f} {statistics.median(r['active'] for r in sub):>6.0f} {sum(c) if c else float('nan'):>7.2f}  {sum(r['failed'] for r in sub)}")

if __name__ == "__main__": main()

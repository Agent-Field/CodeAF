#!/usr/bin/env python3
"""One-road benchmark scoreboard: table + figure.

Reads results/results.csv and any judge_[abc].json per cell; renders
plots/scoreboard.png (light) and a terminal table. Quality = median judge
total (0-20) when >=3 judgements exist, else the deterministic fallback
(test delta + no-regression), always labeled for what it is.
"""
import csv, json, os, statistics, sys
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
from matplotlib.lines import Line2D

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
RES = os.path.join(ROOT, "results")
DIMS = ["requirement_coverage", "correctness", "scope_discipline", "completeness", "report_honesty"]

# Emphasis form: our three arms carry identity hues (validated palette slots
# 1/7/3 — blue, violet, aqua); competitors are context and wear grays with
# distinct shapes, so identity is never color-alone.
ARMS = {  # name -> (label, color, marker, is_ours)
    "aforge-new-flash": ("aforge new (flash)", "#2a78d6", "o", True),
    "aforge-new-crew":  ("aforge new (crew)",  "#4a3aa7", "o", True),
    "aforge-old-flash": ("aforge old",         "#1baf7a", "o", True),
    # The intermediate builds: one hue family, told apart in the table. The
    # final board carries one finalist, so these never need distinct slots.
    "aforge-esc-flash":  ("aforge esc (flash)",  "#6fa3e3", "o", True),
    "aforge-esc-crew":   ("aforge esc (crew)",   "#8d84cf", "o", True),
    "aforge-pre-flash":  ("aforge pre (flash)",  "#4d8fdc", "D", True),
    "aforge-pre-crew":   ("aforge pre (crew)",   "#6c5fbd", "D", True),
    "aforge-ckpt-flash": ("aforge ckpt (flash)", "#2f6fbf", "P", True),
    "aforge-ckpt-crew":  ("aforge ckpt (crew)",  "#4a3aa7", "P", True),
    "aforge-final-crew":  ("aforge final (crew)",  "#1f5fa8", "*", True),
    "aforge-final-flash": ("aforge final (flash)", "#5b8fd6", "*", True),
    "aforge-1g-crew":  ("aforge 1g (crew)",  "#123f7a", "X", True),
    "aforge-1g-flash": ("aforge 1g (flash)", "#3b7ac9", "X", True),
    "aforge-1f-crew":  ("aforge 1f (crew)",  "#2b5f9e", "v", True),
    "aforge-1f-flash": ("aforge 1f (flash)", "#6c9edb", "v", True),
    "aforge-1h-crew":  ("aforge 1h (crew)",  "#0b2f5e", "*", True),
    "aforge-1h-flash": ("aforge 1h (flash)", "#2f6fbf", "*", True),
    "pi":               ("pi",                 "#8a8985", "s", False),
    "opencode":         ("opencode",           "#b5b4b0", "^", False),
}
SURFACE, INK, INK2, GRID = "#fcfcfb", "#0b0b0b", "#52514e", "#e8e7e4"

def tests(s):
    try: p, f = s.split("/"); return int(p), int(f)
    except Exception: return None, None

def judge_total(cell):
    scores = []
    for j in "abcd":
        p = os.path.join(RES, cell, f"judge_{j}.json")
        if not os.path.exists(p): continue
        try:
            raw = open(p).read().strip()
            if raw.startswith("```"): raw = raw.split("\n", 1)[1].rsplit("```", 1)[0]
            o = json.loads(raw)
            if any(o.get("weakened_tests") for o in [o]): return 0, len(scores)+1, True
            scores.append(sum(int(o[d]) for d in DIMS))
        except Exception: pass
    if len(scores) >= 3:
        return statistics.median(scores), len(scores), False
    return None, len(scores), False

rows = list(csv.DictReader(open(os.path.join(RES, "results.csv"))))
# A cell the RUNNER killed mid-task measures the runner, not the harness — it is
# excluded outright rather than scored as a failure.
rows = [r for r in rows if not r["outcome"].startswith("KILLED")]
for r in rows:
    r["cell"] = f'{r["harness"]}-{r["task"]}-{r["seed"]}'
    # A derived active span can only be shorter than the cell's own clock; a
    # rescue that read a bad mtime is caught here rather than plotted.
    wall = float(r["wall_s"] or 0)
    active = float(r["wall_s_active"] or wall)
    r["active"] = active if 0 < active <= wall else wall
    # One price table for every harness: tokens × the model's list price.
    # The billed figure (endpoint-dependent) is kept beside it for aforge.
    r["cost"] = float(r.get("cost_list_usd") or r["cost_usd"]) if (r.get("cost_list_usd") or r["cost_usd"]) else None
    r["billed"] = float(r["cost_usd"]) if r["cost_usd"] else None
    tb, _ = tests(r["tests_before"]); ta, fa = tests(r["tests_after"])
    r["tdelta"] = (ta - tb) if (ta is not None and tb is not None) else 0
    r["regressed"] = (ta is not None and tb is not None and ta < tb) or (fa or 0) > 0
    jt, nj, weak = judge_total(r["cell"])
    r["judge"], r["n_judges"], r["weak"] = jt, nj, weak

tasks = sorted({r["task"] for r in rows}, key=lambda t: (t != "batch", t))
have_judges = sum(1 for r in rows if r["judge"] is not None) >= max(1, len(rows) // 2)

# ── terminal table ──────────────────────────────────────────────────────────
qlabel = "judge/20" if have_judges else "Δtests"
print(f"{'issue':>6} {'arm':22} {'active_s':>8} {'cost_usd':>9} {qlabel:>9} {'files':>5}  notes")
for t in tasks:
    for r in sorted((r for r in rows if r["task"] == t), key=lambda r: r["active"]):
        q = r["judge"] if have_judges else r["tdelta"]
        q = "—" if q is None else q
        c = f'{r["cost"]:.3f}' if r["cost"] is not None else "n/a"
        notes = ("REGRESSED " if r["regressed"] else "") + (r["road"] if r["road"] not in ("n/a", "") else "")
        print(f'{t:>6} {ARMS.get(r["harness"], (r["harness"],))[0]:22} {r["active"]:8.0f} {c:>9} {q!s:>9} {r["changed_files"]:>5}  {notes}')
    print()

# ── figure: 2x2 scatter (time vs quality per issue) + cost panel ────────────
plt.rcParams.update({
    "figure.facecolor": SURFACE, "axes.facecolor": SURFACE,
    "text.color": INK, "axes.edgecolor": GRID, "axes.labelcolor": INK2,
    "xtick.color": INK2, "ytick.color": INK2, "font.size": 10,
    "axes.grid": True, "grid.color": GRID, "grid.linewidth": 0.6,
    "axes.axisbelow": True, "font.family": "DejaVu Sans",
})
n = len(tasks)
ncols = min(n, 4) if n else 1
fig, axes = plt.subplots(1, ncols + 1, figsize=(4.2 * (ncols + 1), 4.6),
                         gridspec_kw={"width_ratios": [1] * ncols + [0.9]})
axes = list(axes) if ncols + 1 > 1 else [axes]

for ax, t in zip(axes[:ncols], tasks):
    sub = [r for r in rows if r["task"] == t]
    for r in sub:
        label, color, marker, ours = ARMS.get(r["harness"], (r["harness"], "#8a8985", "x", False))
        q = r["judge"] if have_judges else r["tdelta"]
        if q is None: continue  # awaiting judges — listed in the table, not plotted
        ax.scatter(r["active"], q, s=110 if ours else 80, c=color, marker=marker,
                   edgecolors=SURFACE, linewidths=2, zorder=3)
        if r["regressed"]:
            ax.scatter(r["active"], q, s=260, facecolors="none", edgecolors="#e34948",
                       linewidths=1.6, zorder=4)
    ax.set_title(f"issue #{t}" if t != "batch" else "batch (4 issues, one ask)",
                 fontsize=11, color=INK, pad=8)
    ax.set_xlabel("active seconds")
    ax.set_xscale("log")
    from matplotlib.ticker import ScalarFormatter, NullFormatter, FixedLocator
    lo = min(r["active"] for r in sub); hi = max(r["active"] for r in sub)
    ticks = [t for t in (30, 60, 120, 240, 480, 960, 1800) if lo * 0.7 <= t <= hi * 1.4] or [round(lo)]
    ax.xaxis.set_major_locator(FixedLocator(ticks))
    ax.xaxis.set_major_formatter(ScalarFormatter())
    ax.xaxis.set_minor_formatter(NullFormatter())
    ax.set_xlim(lo * 0.65, hi * 1.5)
    for s in ("top", "right"): ax.spines[s].set_visible(False)
axes[0].set_ylabel(f"quality — {'blind judge median (0–20)' if have_judges else 'tests added (Δ passing)'}")

# cost panel: aforge arms only (pi/opencode do not self-report cost)
axc = axes[-1]
arm_order = [a for a in ARMS if ARMS[a][3]]
for i, a in enumerate(arm_order):
    costs = [r["cost"] for r in rows if r["harness"] == a and r["cost"] is not None]
    if not costs: continue
    tot = sum(costs)
    axc.bar(i, tot, width=0.62, color=ARMS[a][1], edgecolor=SURFACE, linewidth=2)
    axc.text(i, tot, f"${tot:.2f}", ha="center", va="bottom", fontsize=9, color=INK)
axc.set_xticks(range(len(arm_order)))
axc.set_xticklabels([ARMS[a][0].replace("aforge ", "") for a in arm_order], fontsize=9)
axc.set_title("total cost at list price", fontsize=11, color=INK, pad=8)
axc.set_ylabel("USD across settled cells")
for s in ("top", "right"): axc.spines[s].set_visible(False)
axc.text(0.5, -0.24, "tokens × OpenRouter list price, every harness alike", transform=axc.transAxes,
         ha="center", fontsize=8.5, color=INK2)

handles = [Line2D([], [], linestyle="", marker=m, color=c, markersize=9,
                  markeredgecolor=SURFACE, markeredgewidth=1.5, label=l)
           for l, c, m, _ in ARMS.values()]
handles.append(Line2D([], [], linestyle="", marker="o", markersize=11, markerfacecolor="none",
                      markeredgecolor="#e34948", label="regressed suite"))
fig.legend(handles=handles, loc="lower center", ncol=6, frameon=False, fontsize=9,
           bbox_to_anchor=(0.5, -0.04))
fig.suptitle("One-road benchmark — wave 1 (bambara issues, deepseek-v4-flash)"
             + ("" if have_judges else "  ·  judge scores pending, quality = tests added"),
             fontsize=13, color=INK, y=1.0)
fig.tight_layout(rect=[0, 0.05, 1, 0.97])
out = os.path.join(os.path.dirname(os.path.abspath(__file__)), "scoreboard.png")
fig.savefig(out, dpi=150, bbox_inches="tight", facecolor=SURFACE)
print("wrote", out)

# ── the leader table: one number per arm over the rows that discriminate ──
# #20 is a no-op row (the fix pre-exists), so it is excluded; contaminated and
# rescued cells are excluded by name. Quality is the judge median, or the
# deterministic fallback; cost is summed only where the journal reported it.
REAL = [t for t in tasks if t != "20"]
print(f"\n{'arm':22} {'rows':>4} {'quality/20':>10} {'med active s':>12} {'list $':>7} {'billed $':>9}  regress")
lead = []
for a in ARMS:
    sub = [r for r in rows if r["harness"] == a and r["task"] in REAL]
    if not sub: continue
    def quality(r):
        if not r["outcome"].startswith("OK") or r["regressed"] or r["weak"]: return 0
        q = r["judge"] if have_judges else r["tdelta"]
        return q if q is not None else 0
    q = [quality(r) for r in sub]
    qm = statistics.mean(q); ta = statistics.median(r["active"] for r in sub)
    c = [r["cost"] for r in sub if r["cost"] is not None]
    reg = sum(1 for r in sub if r["regressed"])
    b = [r["billed"] for r in sub if r["billed"] is not None]
    lead.append((qm, a))
    print(f"{ARMS[a][0]:22} {len(sub):>4} {qm:>10.1f} {ta:>12.0f} {sum(c) if c else float('nan'):>7.2f} {sum(b) if b else float('nan'):>9.2f}  {reg}")
print("leader by quality:", ARMS[max(lead)[1]][0] if lead else "—")

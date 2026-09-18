#!/usr/bin/env python3
"""Tweet card: where a run's tokens go against where its dollars go, by role.
Real data: figures/data/seats.csv (132 runs with a per-role split). Medians of
per-run shares, so the two bars do not sum to one; each says what a typical run
looks like. python3 make_anatomy.py <seats.csv>"""
import csv, sys, numpy as np, matplotlib
matplotlib.use("Agg"); import matplotlib.pyplot as plt
INK, ACCENT, MUTED, FILL = "#1a1a1a", "#a0342c", "#8a8a8a", "#d8d8d8"
plt.rcParams.update({"font.family": "serif", "font.serif": ["Times New Roman", "DejaVu Serif"], "font.size": 9, "savefig.dpi": 300})
rows = [r for r in csv.DictReader(open(sys.argv[1])) if float(r["spend"]) > 0]
def share(r, role, kind):
    if kind == "tok":
        tot = sum(int(r["tin_" + s]) + int(r["tout_" + s]) for s in ("work", "check", "plan", "other"))
        return (int(r["tin_" + role]) + int(r["tout_" + role])) / tot if tot else 0
    return float(r["usd_" + role]) / float(r["spend"])
roles = [("work", "writes the code"), ("check", "reads and reviews it"), ("plan", "plans")]
med = {(ro, k): np.mean([share(r, ro, k) for r in rows]) for ro, _ in roles for k in ("tok", "usd")}
fig, ax = plt.subplots(figsize=(6.0, 2.9))
for s in ("top", "right", "left"): ax.spines[s].set_visible(False)
ax.set_yticks([]); ax.set_xlim(0, 1); ax.set_xticks([0, .25, .5, .75, 1]); ax.set_xticklabels(["0", "25%", "50%", "75%", "100%"])
cols = {"work": FILL, "check": ACCENT, "plan": MUTED}
for y, kind, lab in ((1, "tok", "tokens"), (0, "usd", "dollars")):
    x = 0
    for ro, _ in roles:
        v = med[(ro, kind)]
        ax.barh(y, v, left=x, height=0.55, color=cols[ro], edgecolor="white", linewidth=1.5)
        if v > 0.06: ax.text(x + v / 2, y, "%d%%" % round(v * 100), ha="center", va="center", fontsize=8.5,
                             color="white" if ro == "check" else INK)
        x += v
    ax.text(-0.01, y, lab, ha="right", va="center", fontsize=9.5)
ax.text(med[("work", "tok")] / 2, 1.38, "the model that writes the code", ha="center", va="bottom", fontsize=8, color=INK)
ax.text(med[("work", "usd")] + med[("check", "usd")] / 2, -0.62, "the model that only reads it", ha="center", fontsize=8, color=ACCENT)
ax.set_title("Where an agent run's tokens go, and where its dollars go", loc="left", fontsize=10.5, pad=22)
fig.text(0.01, 0.005, "%d graded runs of one production harness, mean per-run share by role, September 2026. Reviewer median share: 2.6%% of tokens, 44%% of dollars." % len(rows), fontsize=6.4, color=MUTED)
fig.tight_layout(rect=(0, 0.05, 1, 1)); fig.savefig("f0-anatomy.png"); fig.savefig("f0-anatomy.pdf")
print({k: round(v, 3) for k, v in med.items()})

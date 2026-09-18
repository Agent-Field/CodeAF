#!/usr/bin/env python3
"""Headline figure for pareto-crewing.tex: dollars per task against accepted
fraction on the executable grade, one marker per crew arm, offline cascades
as diamonds. Reads the DOE output tree written by issue-arm.sh:
    <out>/i<issue>-<arm>/result.json   (arm, issue, work, check, plan, spend_usd, by_model)
    <out>/regrade.jsonl                (grader v2 truth per run: run, commits, pass, pkgs, ...)
    python3 make_headline.py <out-dir>          # real data
    python3 make_headline.py --synthetic        # layout check only, labelled as such
Cascades are computed offline from the paired grades: for arms b -> b' on
the same issue, bill = bill_b + [fail_b] * bill_b', accepted = pass_b or pass_b'.
This is exact when escalation is a fresh run, which is what the DOE runs.
Only numpy and matplotlib; same style block as make_figures.py.
"""
import glob, json, math, os, sys, collections
import numpy as np
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
from matplotlib.ticker import FuncFormatter

INK, ACCENT, MUTED, FILL = "#1a1a1a", "#a0342c", "#8a8a8a", "#d8d8d8"
plt.rcParams.update({"figure.dpi": 140, "savefig.dpi": 300, "font.family": "serif",
    "font.serif": ["Times New Roman", "DejaVu Serif"], "font.size": 8.5,
    "axes.linewidth": 0.6, "axes.edgecolor": INK, "text.color": INK,
    "xtick.labelsize": 7.5, "ytick.labelsize": 7.5, "legend.fontsize": 7.5,
    "legend.frameon": False})

ARM_ORDER = ["frugal", "balanced", "max", "knee", "table"]
ARM_LABEL = {"knee": "old default (knee)", "table": "old table"}

def short(m):
    m = m.split("/")[-1]
    return m.replace("-0902", "").replace("anthropic-", "")

def wilson(k, n, z=1.96):
    if n == 0: return (0.0, 0.0, 1.0)
    p = k / n; d = 1 + z * z / n
    c = (p + z * z / (2 * n)) / d
    h = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / d
    return (p, max(0.0, c - h), min(1.0, c + h))

def load(out):
    """One row per DOE run. Grade truth is <out>/regrade.jsonl (grader v2, offline,
    keys run/commits/pass/...), keyed by the run's directory label; result.json
    supplies arm, issue, crew and bill. A run is accepted when it landed at
    least one commit AND the regrade passes. grade-last.json is NOT read: it is
    the in-run grade at whatever grader the binary carried."""
    regrade = {}
    p = os.path.join(out, "regrade.jsonl")
    if os.path.exists(p):
        for line in open(p):
            line = line.strip()
            if not line: continue
            g = json.loads(line)
            if g.get("gold"): continue
            regrade[g["run"]] = g
    rows = []
    for d in sorted(glob.glob(os.path.join(out, "i*-*"))):
        label = os.path.basename(d.rstrip("/"))
        r = os.path.join(d, "result.json")
        if not os.path.exists(r) or label not in regrade: continue
        R, G = json.load(open(r)), regrade[label]
        if G.get("graded") is False or (G.get("pkgs", None) == "" and G.get("changed", 0) > 0): continue  # absent
        arm = R.get("arm", label)
        arm = arm.split("-", 1)[1] if arm.startswith("i") and "-" in arm else arm
        rows.append(dict(issue=int(R["issue"]), arm=arm, spend=float(R["spend_usd"]),
                         passed=bool(G.get("pass")) and int(G.get("commits", 0)) > 0,
                         work=R.get("work", "auto"), check=R.get("check", "auto"),
                         by_model=R.get("by_model", {})))
    return rows

def synthetic(seed=3, n_issues=36):
    rng = np.random.default_rng(seed)
    truth = {"frugal": (0.45, 0.38), "balanced": (0.65, 0.62), "max": (2.4, 0.72), "knee": (2.6, 0.70)}
    crews = {"frugal": ("deepseek-v4.1-flash", "qwen3.8-max"), "balanced": ("deepseek-v4.1-flash", "kimi-k3"),
             "max": ("glm-5.3", "claude-fable-5.1"), "knee": ("glm-5.3-flash", "claude-fable-5.1")}
    rows = []
    for i in range(n_issues):
        hard = rng.normal(0, 0.8)
        for arm, (bill, p) in truth.items():
            logit = math.log(p / (1 - p)) - hard
            pp = 1 / (1 + math.exp(-logit))
            rows.append(dict(issue=i, arm=arm, spend=float(rng.lognormal(math.log(bill), 0.6)),
                             passed=bool(rng.random() < pp), work=crews[arm][0], check=crews[arm][1], by_model={}))
    return rows

def summarise(rows):
    by = collections.defaultdict(list)
    for r in rows: by[r["arm"]].append(r)
    arms = {}
    for arm, rs in by.items():
        k = sum(r["passed"] for r in rs); n = len(rs)
        p, lo, hi = wilson(k, n)
        names = collections.Counter((short(r["work"]), short(r["check"])) for r in rs).most_common(1)[0][0]
        arms[arm] = dict(bill=float(np.mean([r["spend"] for r in rs])), p=p, lo=lo, hi=hi, n=n, k=k, names=names)
    return arms

def cascades(rows, pairs):
    idx = {(r["issue"], r["arm"]): r for r in rows}
    out = {}
    for b, b2 in pairs:
        issues = sorted({i for (i, a) in idx if a == b} & {i for (i, a) in idx if a == b2})
        if not issues: continue
        bills, passes = [], []
        for i in issues:
            r1, r2 = idx[(i, b)], idx[(i, b2)]
            bills.append(r1["spend"] + (0 if r1["passed"] else r2["spend"]))
            passes.append(r1["passed"] or r2["passed"])
        k, n = sum(passes), len(issues); p, lo, hi = wilson(k, n)
        out[(b, b2)] = dict(bill=float(np.mean(bills)), p=p, lo=lo, hi=hi, n=n, k=k)
    return out

def draw(arms, casc, n_issues, synthetic_flag, stem):
    fig, ax = plt.subplots(figsize=(6.3, 3.9))
    for s in ("top", "right"): ax.spines[s].set_visible(False)
    ax.set_xscale("log")
    xs = [a["bill"] for a in arms.values()] + [c["bill"] for c in casc.values()]
    ax.set_xlim(min(xs) / 1.8, max(xs) * 2.2); ax.set_ylim(0, 1.0)
    ticks = [t for t in (0.1, 0.25, 0.5, 1, 2, 5, 10, 20) if min(xs) / 1.8 <= t <= max(xs) * 2.2]
    ax.set_xticks(ticks); ax.set_xticklabels(["$%g" % t for t in ticks]); ax.minorticks_off()
    ax.yaxis.set_major_formatter(FuncFormatter(lambda y, _: "%d%%" % round(y * 100)))
    ax.set_xlabel("dollars per task (mean bill, log scale)")
    ax.set_ylabel("tasks accepted by the executable grade")
    ax.grid(axis="y", color=FILL, lw=0.5); ax.set_axisbelow(True)
    # lower hull of single crews: sort by bill, keep points that raise acceptance
    pts = sorted(((a["bill"], a["p"], k) for k, a in arms.items()))
    hull = []; best = -1
    for b, p, k in pts:
        if p > best: hull.append((b, p)); best = p
    if len(hull) > 1:
        ax.plot([h[0] for h in hull], [h[1] for h in hull], color=MUTED, lw=0.8, ls=(0, (3, 3)), zorder=2)
    for arm in ARM_ORDER:
        if arm not in arms: continue
        a = arms[arm]
        ax.errorbar(a["bill"], a["p"], yerr=[[a["p"] - a["lo"]], [a["hi"] - a["p"]]], fmt="none",
                    ecolor=MUTED, elinewidth=0.7, capsize=0, zorder=3)
        ax.scatter(a["bill"], a["p"], s=26 + 1.6 * a["n"], marker="s", facecolor="white", edgecolor=INK,
                   linewidths=0.9, zorder=4)
        ax.annotate("%s\n%s writes, %s reviews\n%d/%d" % (ARM_LABEL.get(arm, arm), a["names"][0], a["names"][1], a["k"], a["n"]),
                    (a["bill"], a["p"]), xytext=(7, -4), textcoords="offset points", fontsize=6.4, color=INK, va="top")
    win = None
    for j, ((b, b2), c) in enumerate(sorted(casc.items(), key=lambda kv: kv[1]["bill"])):
        dy = 8 if j % 2 == 0 else -8
        ax.errorbar(c["bill"], c["p"], yerr=[[c["p"] - c["lo"]], [c["hi"] - c["p"]]], fmt="none",
                    ecolor=ACCENT, elinewidth=0.7, zorder=3)
        ax.scatter(c["bill"], c["p"], s=26 + 1.6 * c["n"], marker="D", color=ACCENT, zorder=5)
        ax.annotate("%s, then %s if the grade fails\n%d/%d" % (b, b2, c["k"], c["n"]), (c["bill"], c["p"]),
                    xytext=(7, dy), textcoords="offset points", fontsize=6.4, color=ACCENT, va="bottom" if dy > 0 else "top")
        ref = arms.get(b2)
        if ref and c["p"] >= ref["p"] - 1e-9 and c["bill"] < ref["bill"] and (win is None or c["bill"] < win[1]["bill"]):
            win = ((b, b2), c, ref)
    if win:
        (b, b2), c, ref = win
        saving = 1 - c["bill"] / ref["bill"]
        ax.annotate("", xy=(c["bill"], c["p"]), xytext=(ref["bill"], ref["p"]),
                    arrowprops=dict(arrowstyle="->", color=ACCENT, lw=1.0, shrinkA=8, shrinkB=8), zorder=6)
        ax.text(math.sqrt(c["bill"] * ref["bill"]), max(c["hi"], ref["hi"]) + 0.03,
                "same or better acceptance,\n%d%% cheaper" % round(saving * 100), ha="center", fontsize=7.5,
                color=ACCENT, zorder=7)
        head = "The cheap crew plus a compiler check beats the expensive crew"
    else:
        head = "What each crew costs, and how often the grade accepts its work"
    ax.set_title(head, loc="left", fontsize=9.5, pad=8)
    foot = "%d GitHub issues from the codeaf repo, one run per issue per crew, executable grade (gofmt, build, vet, tests), September 2026" % n_issues
    if synthetic_flag:
        foot = "SYNTHETIC LAYOUT CHECK. No measurement here."
        ax.text(0.5, 0.5, "SYNTHETIC", transform=ax.transAxes, fontsize=40, color=FILL, ha="center", va="center", zorder=0)
    fig.text(0.01, 0.01, foot, fontsize=6.4, color=MUTED)
    fig.tight_layout(rect=(0, 0.03, 1, 1))
    fig.savefig(stem + ".pdf"); fig.savefig(stem + ".png")

def main():
    if "--synthetic" in sys.argv:
        rows, syn = synthetic(), True
    else:
        rows, syn = load(sys.argv[1]), False
    if not rows: sys.exit("no graded runs found")
    arms = summarise(rows)
    order = [a for a in ARM_ORDER if a in arms and a not in ("knee", "table")]
    casc = cascades(rows, [(order[i], order[j]) for i in range(len(order)) for j in range(i + 1, len(order))])
    n_issues = len({r["issue"] for r in rows})
    for k, a in arms.items(): print("%-9s bill %.2f  acc %d/%d  [%.2f, %.2f]  %s" % (k, a["bill"], a["k"], a["n"], a["lo"], a["hi"], a["names"]))
    for k, c in casc.items(): print("%-18s bill %.2f  acc %d/%d  [%.2f, %.2f]" % ("->".join(k), c["bill"], c["k"], c["n"], c["lo"], c["hi"]))
    draw(arms, casc, n_issues, syn, "f0-headline")
main()

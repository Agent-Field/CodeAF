"""Probe lab analysis.

Positive class throughout = FAILURE ("this model cannot do this task -> escalate").
That orientation is what the router needs: we are detecting trouble, not success.

Outputs: analysis.json, plots/*.png, and a printed report body.
"""
from __future__ import annotations

import json
import math
import os
from collections import defaultdict

import numpy as np

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt  # noqa: E402

from sklearn.ensemble import HistGradientBoostingClassifier  # noqa: E402
from sklearn.linear_model import LogisticRegression  # noqa: E402
from sklearn.pipeline import make_pipeline  # noqa: E402
from sklearn.preprocessing import StandardScaler  # noqa: E402

OUT = "plots"
os.makedirs(OUT, exist_ok=True)
RNG = np.random.default_rng(0)

# --------------------------------------------------------------------------
# load
# --------------------------------------------------------------------------
ROWS = [json.loads(l) for l in open("results.jsonl")]
MODELS = sorted({r["model"] for r in ROWS})
TASKS = sorted({r["task"] for r in ROWS})
PRICE = {"deepseek-v4-flash": (0.09, 0.18), "qwen3-30b-a3b": (0.048, 0.193),
         "glm-4.7": (0.40, 1.75)}

for r in ROWS:
    r["fail"] = 0 if r["solved"] else 1  # positive class = failure

print("=" * 78)
print("PROBE LAB ANALYSIS")
print("=" * 78)
print(f"rows={len(ROWS)}  models={MODELS}  tasks={len(TASKS)}")
print(f"failure base rate = {sum(r['fail'] for r in ROWS)}/{len(ROWS)} "
      f"= {np.mean([r['fail'] for r in ROWS]):.3f}")

# --------------------------------------------------------------------------
# AUROC with a bootstrap CI
# --------------------------------------------------------------------------

def auroc(y, s):
    """Mann-Whitney AUROC. y=1 is the positive (failure) class."""
    y, s = np.asarray(y, float), np.asarray(s, float)
    m = ~np.isnan(s)
    y, s = y[m], s[m]
    pos, neg = s[y == 1], s[y == 0]
    if len(pos) == 0 or len(neg) == 0:
        return float("nan"), 0, 0
    order = np.argsort(s)
    ranks = np.empty(len(s), float)
    sr = s[order]
    i = 0
    while i < len(sr):
        j = i
        while j + 1 < len(sr) and sr[j + 1] == sr[i]:
            j += 1
        ranks[order[i:j + 1]] = (i + j) / 2.0 + 1
        i = j + 1
    n1, n0 = len(pos), len(neg)
    a = (ranks[y == 1].sum() - n1 * (n1 + 1) / 2) / (n1 * n0)
    return a, n1, n0


def auroc_ci(y, s, n_boot=2000):
    y, s = np.asarray(y, float), np.asarray(s, float)
    m = ~np.isnan(s)
    y, s = y[m], s[m]
    if len(np.unique(y)) < 2:
        return float("nan"), float("nan")
    vals = []
    for _ in range(n_boot):
        idx = RNG.integers(0, len(y), len(y))
        if len(np.unique(y[idx])) < 2:
            continue
        a, _, _ = auroc(y[idx], s[idx])
        vals.append(a)
    return (float(np.percentile(vals, 2.5)), float(np.percentile(vals, 97.5))) if vals else (float("nan"),) * 2


# --------------------------------------------------------------------------
# 1. per-signal predictive power
# --------------------------------------------------------------------------
# Each signal is oriented so that HIGHER = MORE LIKELY TO FAIL.
SIGNALS = {
    "self_consistency (1-agreement)": ("sc_agree", -1, "self_consistency"),
    "sc_secondary (1-majority/charsim)": ("sc_major", -1, "self_consistency"),
    "sc_length_cv": ("sc_len_cv", +1, "self_consistency"),
    "sketch_instability (1-jaccard)": ("sketch_jaccard", -1, "sketch"),
    "sketch_instability (1-charsim)": ("sketch_charsim", -1, "sketch"),
    "verbalized_conf (1-conf)": ("vconf", -1, "vconf"),
    "logprob mean (negated)": ("lp_mean_logprob", -1, "logprob"),
    "logprob min (negated)": ("lp_min_logprob", -1, "logprob"),
    "logprob p10 (negated)": ("lp_p10_logprob", -1, "logprob"),
    "logprob frac<-1": ("lp_frac_below_1", +1, "logprob"),
    "logprob mean entropy": ("lp_mean_entropy", +1, "logprob"),
    "logprob max entropy": ("lp_max_entropy", +1, "logprob"),
}
# deterministic task features, for reference
FEATS = {
    "prompt_chars": ("prompt_chars", +1),
    "n_tests": ("n_tests", +1),
    "schema_depth": ("schema_depth", +1),
    "n_constraint_words": ("n_constraint_words", +1),
    "n_digits": ("n_digits", +1),
}


def sig_vec(rows, key, sign):
    out = []
    for r in rows:
        v = r["signals"].get(key)
        out.append(np.nan if v is None else sign * float(v))
    return np.array(out, float)


print("\n" + "-" * 78)
print("1. PER-SIGNAL AUROC for predicting FAILURE (higher score = predicted failure)")
print("-" * 78)

# cost per probe family, per cell, averaged over models
fam_cost = defaultdict(list)
for r in ROWS:
    for fam, c in r["probe"]["per_signal_cost"].items():
        fam_cost[fam].append(c)
FAM_COST = {k: float(np.mean(v)) for k, v in fam_cost.items()}

signal_table = []
print(f"{'signal':<36}{'pooled AUROC':<24}{'n':<6}{'$/probe':<11}{'AUROC-1/2 per $':<16}cov")
for name, (key, sign, fam) in SIGNALS.items():
    y = np.array([r["fail"] for r in ROWS], float)
    s = sig_vec(ROWS, key, sign)
    cov = int(np.sum(~np.isnan(s)))
    a, n1, n0 = auroc(y, s)
    lo, hi = auroc_ci(y, s)
    c = FAM_COST[fam]
    lift = (a - 0.5) if not math.isnan(a) else float("nan")
    per_dollar = lift / c if c > 0 else float("nan")
    signal_table.append(dict(signal=name, family=fam, auroc=a, ci_lo=lo, ci_hi=hi,
                             coverage=cov, cost_per_probe=c, auroc_lift=lift,
                             lift_per_dollar=per_dollar))
    print(f"{name:<36}{a:.3f} [{lo:.3f},{hi:.3f}]   {cov:<6}{c:.6f}   {per_dollar:>13.1f}  {cov}/{len(ROWS)}")

print("\nper-model AUROC (pooled signals may hide model-specific behaviour)")
per_model_auroc = {}
hdr = f"{'signal':<36}" + "".join(f"{m[:15]:<17}" for m in MODELS)
print(hdr)
for name, (key, sign, fam) in SIGNALS.items():
    cells = []
    for m in MODELS:
        rs = [r for r in ROWS if r["model"] == m]
        a, n1, n0 = auroc([r["fail"] for r in rs], sig_vec(rs, key, sign))
        per_model_auroc[(name, m)] = a
        cells.append("n/a" if math.isnan(a) else f"{a:.3f} ({n1}f)")
    print(f"{name:<36}" + "".join(f"{c:<17}" for c in cells))

print("\ndeterministic task features (free -- no API call)")
for name, (key, sign) in FEATS.items():
    y = np.array([r["fail"] for r in ROWS], float)
    s = np.array([sign * float(r["features"][key]) for r in ROWS], float)
    a, _, _ = auroc(y, s)
    lo, hi = auroc_ci(y, s)
    print(f"  {name:<26}AUROC {a:.3f} [{lo:.3f},{hi:.3f}]")
    signal_table.append(dict(signal="feat:" + name, family="features", auroc=a,
                             ci_lo=lo, ci_hi=hi, coverage=len(ROWS), cost_per_probe=0.0,
                             auroc_lift=a - 0.5, lift_per_dollar=float("inf")))

# --------------------------------------------------------------------------
# 2. classifiers, leave-one-TASK-out CV
# --------------------------------------------------------------------------
print("\n" + "-" * 78)
print("2. CLASSIFIERS (leave-one-task-out CV: all 3 rows of a task are held out together)")
print("-" * 78)

PROBE_COLS = [("sc_agree", 1), ("sc_major", 1), ("sc_len_cv", 1),
              ("sketch_jaccard", 1), ("sketch_charsim", 1), ("vconf", 1)]
LP_COLS = [("lp_mean_logprob", 1), ("lp_frac_below_1", 1), ("lp_mean_entropy", 1)]
FEAT_COLS = ["prompt_chars", "prompt_words", "cls_code", "cls_json", "cls_exact",
             "n_tests", "schema_depth", "numeric_constraints", "n_digits",
             "n_constraint_words"]


def build(kind):
    """Return X, y, groups(task), and the column names."""
    X, names = [], []
    for r in ROWS:
        row = []
        if kind in ("features", "combined", "combined+model"):
            row += [float(r["features"][c]) for c in FEAT_COLS]
        if kind in ("probes", "combined", "combined+model"):
            for k, _ in PROBE_COLS:
                v = r["signals"].get(k)
                row.append(np.nan if v is None else float(v))
            for k, _ in LP_COLS:
                v = r["signals"].get(k)
                row.append(np.nan if v is None else float(v))
            row.append(1.0 if r["signals"]["lp_available"] else 0.0)
        if kind == "combined+model":
            row += [1.0 if r["model"] == m else 0.0 for m in MODELS]
        X.append(row)
    if kind in ("features", "combined", "combined+model"):
        names += FEAT_COLS
    if kind in ("probes", "combined", "combined+model"):
        names += [k for k, _ in PROBE_COLS] + [k for k, _ in LP_COLS] + ["lp_available"]
    if kind == "combined+model":
        names += [f"model={m}" for m in MODELS]
    y = np.array([r["fail"] for r in ROWS], float)
    g = np.array([r["task"] for r in ROWS])
    return np.array(X, float), y, g, names


def impute(train, test):
    """Median-impute from TRAIN only (logprobs are missing for 2 of 3 models)."""
    med = np.nanmedian(train, axis=0)
    med = np.where(np.isnan(med), 0.0, med)
    tr = np.where(np.isnan(train), med, train)
    te = np.where(np.isnan(test), med, test)
    return tr, te


def cv_scores(kind, model_name):
    X, y, g, names = build(kind)
    oof = np.full(len(y), np.nan)
    for t in np.unique(g):
        te = g == t
        tr = ~te
        if len(np.unique(y[tr])) < 2:
            oof[te] = y[tr].mean()
            continue
        Xtr, Xte = impute(X[tr], X[te])
        if model_name == "logreg":
            clf = make_pipeline(StandardScaler(),
                                LogisticRegression(C=0.3, max_iter=5000,
                                                   class_weight="balanced"))
        else:
            clf = HistGradientBoostingClassifier(
                max_depth=3, max_iter=120, learning_rate=0.08,
                min_samples_leaf=5, l2_regularization=1.0, random_state=0)
        clf.fit(Xtr, y[tr])
        oof[te] = clf.predict_proba(Xte)[:, 1]
    return oof, y, names


RESULTS = {}
print(f"{'feature set':<22}{'model':<10}{'AUROC (LOTO-CV)':<26}{'Brier':<9}{'acc@0.5'}")
for kind in ("features", "probes", "combined", "combined+model"):
    for mn in ("logreg", "gbt"):
        oof, y, names = cv_scores(kind, mn)
        a, _, _ = auroc(y, oof)
        lo, hi = auroc_ci(y, oof)
        brier = float(np.mean((oof - y) ** 2))
        acc = float(np.mean((oof >= 0.5) == (y == 1)))
        RESULTS[(kind, mn)] = dict(oof=oof, auroc=a, ci=(lo, hi), brier=brier, acc=acc)
        print(f"{kind:<22}{mn:<10}{a:.3f} [{lo:.3f},{hi:.3f}]        {brier:.3f}    {acc:.3f}")

# baseline: the single best probe signal alone
best_sig = max([s for s in signal_table if s["family"] != "features"],
               key=lambda d: d["auroc"] if not math.isnan(d["auroc"]) else -1)
print(f"\n(best single probe signal for reference: {best_sig['signal']} AUROC {best_sig['auroc']:.3f})")

BEST_KEY = max(RESULTS, key=lambda k: RESULTS[k]["auroc"])
print(f"BEST classifier: {BEST_KEY[0]} / {BEST_KEY[1]}  AUROC={RESULTS[BEST_KEY]['auroc']:.3f}")
best_oof = RESULTS[BEST_KEY]["oof"]
y_all = np.array([r["fail"] for r in ROWS], float)

# --------------------------------------------------------------------------
# 3. asymmetric operating point
# --------------------------------------------------------------------------
print("\n" + "-" * 78)
print("3. ASYMMETRIC OPERATING POINT (positive = escalate)")
print("-" * 78)


def op_point(scores, y, thr):
    esc = scores >= thr
    tp = int(np.sum(esc & (y == 1)))
    fp = int(np.sum(esc & (y == 0)))
    fn = int(np.sum(~esc & (y == 1)))
    tn = int(np.sum(~esc & (y == 0)))
    prec = tp / (tp + fp) if tp + fp else float("nan")
    rec = tp / (tp + fn) if tp + fn else float("nan")
    solvable_kept = tn / (tn + fp) if tn + fp else float("nan")  # truly-solvable NOT escalated
    return dict(thr=float(thr), tp=tp, fp=fp, fn=fn, tn=tn, escalation_precision=prec,
                failure_recall=rec, solvable_retained=solvable_kept,
                solvable_wrongly_escalated=1 - solvable_kept if not math.isnan(solvable_kept) else float("nan"),
                escalation_rate=float(np.mean(esc)))


# threshold where >= 95% of truly-solvable tasks are NOT escalated
cands = np.unique(np.concatenate([best_oof, [0.0, 1.01]]))
best95 = None
for thr in sorted(cands):
    o = op_point(best_oof, y_all, thr)
    if o["solvable_retained"] >= 0.95:
        best95 = o
        break
print("threshold with >=95% of truly-solvable tasks NOT escalated:")
if best95:
    print(f"  threshold                          = {best95['thr']:.4f}")
    print(f"  escalation precision               = {best95['escalation_precision']:.3f} "
          f"({best95['tp']}/{best95['tp']+best95['fp']})")
    print(f"  failure recall (caught)            = {best95['failure_recall']:.3f} "
          f"({best95['tp']}/{best95['tp']+best95['fn']})")
    print(f"  truly-solvable wrongly escalated   = {best95['solvable_wrongly_escalated']:.3f} "
          f"({best95['fp']}/{best95['fp']+best95['tn']})")
    print(f"  overall escalation rate            = {best95['escalation_rate']:.3f}")

# Chow rule: escalate when expected cost of escalating < expected cost of attempting.
# escalate costs 5x a cheap attempt; a failure costs 20x. Attempt EV = p*20 + (1-p)*1 ... in
# units of one cheap attempt; escalate EV = 5. Indifference: p*20 + (1-p)*1 = 5 -> p = 4/19.
C_ESC, C_FAIL, C_ATT = 5.0, 20.0, 1.0
chow = (C_ESC - C_ATT) / (C_FAIL - C_ATT)
print(f"\nChow rule with escalate={C_ESC}x, failure={C_FAIL}x, cheap attempt={C_ATT}x:")
print(f"  indifference threshold p* = ({C_ESC}-{C_ATT})/({C_FAIL}-{C_ATT}) = {chow:.4f}")
ochow = op_point(best_oof, y_all, chow)
print(f"  escalation precision               = {ochow['escalation_precision']:.3f} "
      f"({ochow['tp']}/{ochow['tp']+ochow['fp']})")
print(f"  failure recall                     = {ochow['failure_recall']:.3f}")
print(f"  truly-solvable wrongly escalated   = {ochow['solvable_wrongly_escalated']:.3f}")
print(f"  escalation rate                    = {ochow['escalation_rate']:.3f}")


def expected_cost(scores, y, thr):
    esc = scores >= thr
    tot = 0.0
    for e, f in zip(esc, y):
        tot += C_ESC if e else (C_FAIL if f == 1 else C_ATT)
    return tot / len(y)


print(f"\n  expected cost/task (units of one cheap attempt):")
print(f"    always-attempt      = {expected_cost(best_oof, y_all, 1e9):.3f}")
print(f"    always-escalate     = {C_ESC:.3f}")
print(f"    Chow-threshold      = {expected_cost(best_oof, y_all, chow):.3f}")
grid = np.linspace(0, 1, 201)
best_thr = min(grid, key=lambda t: expected_cost(best_oof, y_all, t))
print(f"    oracle-tuned thr {best_thr:.3f} = {expected_cost(best_oof, y_all, best_thr):.3f}")

# --------------------------------------------------------------------------
# 4. calibration
# --------------------------------------------------------------------------
print("\n" + "-" * 78)
print("4. CALIBRATION")
print("-" * 78)


def reliability(scores, y, bins=5):
    edges = np.linspace(0, 1, bins + 1)
    out = []
    for i in range(bins):
        m = (scores >= edges[i]) & (scores < edges[i + 1] + (1e-9 if i == bins - 1 else 0))
        if m.sum():
            out.append((float(scores[m].mean()), float(y[m].mean()), int(m.sum())))
    return out


def ece(scores, y, bins=5):
    r = reliability(scores, y, bins)
    n = len(y)
    return sum(cnt / n * abs(p - o) for p, o, cnt in r)


print(f"best classifier ({BEST_KEY[0]}/{BEST_KEY[1]}) reliability (predicted P(fail) vs observed):")
rel_clf = reliability(best_oof, y_all)
for p, o, n in rel_clf:
    print(f"  pred {p:.3f}  observed {o:.3f}  n={n}")
print(f"  ECE = {ece(best_oof, y_all):.3f}   Brier = {RESULTS[BEST_KEY]['brier']:.3f}")

vc = np.array([r["signals"]["vconf"] if r["signals"]["vconf"] is not None else np.nan
               for r in ROWS], float)
mv = ~np.isnan(vc)
vc_fail_pred = 1 - vc[mv]  # model's implied P(fail)
print(f"\nverbalized confidence as a probability of success (n={mv.sum()}):")
rel_vc = reliability(vc_fail_pred, y_all[mv])
for p, o, n in rel_vc:
    print(f"  implied P(fail) {p:.3f}  observed {o:.3f}  n={n}")
print(f"  ECE = {ece(vc_fail_pred, y_all[mv]):.3f}   Brier = {np.mean((vc_fail_pred - y_all[mv])**2):.3f}")
print(f"  mean stated confidence = {np.mean(vc[mv]):.3f}  vs  actual success rate = "
      f"{1-np.mean(y_all[mv]):.3f}   -> overconfidence gap = {np.mean(vc[mv]) - (1-np.mean(y_all[mv])):.3f}")

# --------------------------------------------------------------------------
# 5. economics simulation
# --------------------------------------------------------------------------
print("\n" + "-" * 78)
print("5. POLICY / ECONOMICS SIMULATION (real measured $ per call)")
print("-" * 78)

CHEAP = min(MODELS, key=lambda m: np.mean([r["attempt"]["cost"] for r in ROWS if r["model"] == m]))
by_mt = {(r["model"], r["task"]): r for r in ROWS}
solve_rate = {m: np.mean([r["solved"] for r in ROWS if r["model"] == m]) for m in MODELS}
STRONG = max(MODELS, key=lambda m: solve_rate[m])
print(f"cheap model  = {CHEAP} (mean attempt ${np.mean([r['attempt']['cost'] for r in ROWS if r['model']==CHEAP]):.5f}, "
      f"solve {solve_rate[CHEAP]:.3f})")
print(f"strong model = {STRONG} (mean attempt ${np.mean([r['attempt']['cost'] for r in ROWS if r['model']==STRONG]):.5f}, "
      f"solve {solve_rate[STRONG]:.3f})")
for m in MODELS:
    print(f"   {m:<20} solve={solve_rate[m]:.3f} mean_attempt=$"
          f"{np.mean([r['attempt']['cost'] for r in ROWS if r['model']==m]):.5f} "
          f"mean_probe=${np.mean([r['probe']['cost'] for r in ROWS if r['model']==m]):.5f}")

# Router scores for the cheap model only, from LOTO-CV, trained on cheap-model rows only.
cheap_rows = [r for r in ROWS if r["model"] == CHEAP]


def cv_scores_subset(kind, model_name, rows):
    idx = [i for i, r in enumerate(ROWS) if r["model"] == CHEAP]
    X, y, g, _ = build(kind)
    Xs, ys, gs = X[idx], y[idx], g[idx]
    oof = np.full(len(ys), np.nan)
    for t in np.unique(gs):
        te = gs == t
        tr = ~te
        if len(np.unique(ys[tr])) < 2:
            oof[te] = ys[tr].mean()
            continue
        Xtr, Xte = impute(Xs[tr], Xs[te])
        clf = (make_pipeline(StandardScaler(), LogisticRegression(C=0.3, max_iter=5000,
                                                                  class_weight="balanced"))
               if model_name == "logreg" else
               HistGradientBoostingClassifier(max_depth=3, max_iter=120, learning_rate=0.08,
                                              min_samples_leaf=5, l2_regularization=1.0,
                                              random_state=0))
        clf.fit(Xtr, ys[tr])
        oof[te] = clf.predict_proba(Xte)[:, 1]
    return oof, ys


route_oof, route_y = cv_scores_subset("combined", "logreg", cheap_rows)
ROUTER_AUROC = auroc(route_y, route_oof)[0]
print(f"\nrouter (cheap-model-only, combined features, LOTO-CV) AUROC = "
      f"{ROUTER_AUROC:.3f}  (n={len(route_y)}, failures={int(route_y.sum())})")
ro = op_point(route_oof, route_y, chow)
print(f"  at the Chow threshold {chow:.3f}: escalates {ro['tp']+ro['fp']}/24 tasks, "
      f"precision={ro['escalation_precision']:.3f}, recall={ro['failure_recall']:.3f}, "
      f"solvable wrongly escalated={ro['solvable_wrongly_escalated']:.3f}")
if ro["fn"] == 0:
    print("  recall is 1.0, so no cheap-model failure survives routing -- which is why")
    print("  'probe-then-route' and 'probe-route-then-retry' score identically below.")
ROUTER_OP = ro


def policy_costs(thr=None, mode="cheap"):
    """Return (success_rate, mean_cost_$) over the 24 tasks."""
    succ, cost = [], []
    for i, r in enumerate(cheap_rows):
        t = r["task"]
        cheap_r, strong_r = by_mt[(CHEAP, t)], by_mt[(STRONG, t)]
        if mode == "cheap":
            succ.append(cheap_r["solved"])
            cost.append(cheap_r["attempt"]["cost"])
        elif mode == "strong":
            succ.append(strong_r["solved"])
            cost.append(strong_r["attempt"]["cost"])
        elif mode == "probe_route":
            c = cheap_r["probe"]["cost"]
            if route_oof[i] >= thr:
                succ.append(strong_r["solved"])
                cost.append(c + strong_r["attempt"]["cost"])
            else:
                succ.append(cheap_r["solved"])
                cost.append(c + cheap_r["attempt"]["cost"])
        elif mode == "cheap_then_strong":  # no probe: try cheap, escalate on failure
            if cheap_r["solved"]:
                succ.append(True)
                cost.append(cheap_r["attempt"]["cost"])
            else:
                succ.append(strong_r["solved"])
                cost.append(cheap_r["attempt"]["cost"] + strong_r["attempt"]["cost"])
        elif mode == "oracle":
            if cheap_r["solved"]:
                succ.append(True)
                cost.append(cheap_r["attempt"]["cost"])
            else:
                succ.append(strong_r["solved"])
                cost.append(strong_r["attempt"]["cost"])
    return float(np.mean(succ)), float(np.mean(cost))


print(f"\n{'policy':<34}{'success':<11}{'mean $/task':<15}{'$ per success'}")
pol = {}
for label, mode, thr in [("always-cheap", "cheap", None),
                         ("always-strongest-of-3", "strong", None),
                         ("cheap-then-escalate-on-failure", "cheap_then_strong", None),
                         ("probe-then-route (Chow p*)", "probe_route", chow),
                         ("probe-then-route (thr 0.5)", "probe_route", 0.5),
                         ("oracle route (upper bound)", "oracle", None)]:
    s, c = policy_costs(thr, mode)
    pol[label] = dict(success=s, mean_cost=c, cost_per_success=c / s if s else float("nan"))
    print(f"{label:<34}{s:<11.3f}${c:<14.5f}${c/s if s else float('nan'):.5f}")

# probe overhead
probe_mean = float(np.mean([r["probe"]["cost"] for r in cheap_rows]))
att_mean = float(np.mean([r["attempt"]["cost"] for r in cheap_rows]))
print(f"\nprobe overhead on the cheap model: ${probe_mean:.5f} probe vs ${att_mean:.5f} attempt "
      f"= {100*probe_mean/att_mean:.1f}% of attempt cost")
for m in MODELS:
    p = float(np.mean([r["probe"]["cost"] for r in ROWS if r["model"] == m]))
    a = float(np.mean([r["attempt"]["cost"] for r in ROWS if r["model"] == m]))
    print(f"   {m:<20} probe/attempt = {100*p/a:5.1f}%   (probe ${p:.5f}, attempt ${a:.5f})")

# break-even: probe pays for itself when saved failure cost > probe cost
print("\nbreak-even analysis (does the probe pay for itself?)")
base_s, base_c = pol["always-cheap"]["success"], pol["always-cheap"]["mean_cost"]
pr_s, pr_c = pol["probe-then-route (Chow p*)"]["success"], pol["probe-then-route (Chow p*)"]["mean_cost"]
print(f"  probe-then-route buys {pr_s-base_s:+.3f} success for {pr_c-base_c:+.5f} $/task")
if pr_s > base_s:
    print(f"  -> ${(pr_c-base_c)/(pr_s-base_s):.5f} per extra solved task")

# --------------------------------------------------------------------------
# 6. break-even: how expensive must a task be before probing pays for itself?
# --------------------------------------------------------------------------
print("\n" + "-" * 78)
print("6. BREAK-EVEN: at what task cost does the probe pay for itself?")
print("-" * 78)
print("Probe cost is ~fixed (7 short calls, output capped at 96-256 tokens); attempt cost")
print("scales with the size of the job. Sweep k = attempt-cost multiplier (k=1 is measured).")


print("\n'$ per success' is the wrong lens on its own: it never charges for a failure, so")
print("always-cheap wins by simply failing a third of the time for free. The decision-relevant")
print("quantity is total expected cost INCLUDING a downstream cost F for each unsolved task")
print("(a failed agent step costs a retry loop, a human, or a bad artifact). Sweep F.")

POLICY_MODES = ["always-cheap", "always-strong", "cheap-then-escalate",
                "probe-then-route", "probe-route-then-retry"]


def policy_at(k, F, thr):
    """Scale ATTEMPT costs by k, keep probe cost fixed, charge F dollars per unsolved task."""
    out = {}
    for label in POLICY_MODES:
        succ, cost = [], []
        for i, r in enumerate(cheap_rows):
            t = r["task"]
            cr, sr = by_mt[(CHEAP, t)], by_mt[(STRONG, t)]
            ca, sa = cr["attempt"]["cost"] * k, sr["attempt"]["cost"] * k
            p = cr["probe"]["cost"]
            if label == "always-cheap":
                s, c = cr["solved"], ca
            elif label == "always-strong":
                s, c = sr["solved"], sa
            elif label == "cheap-then-escalate":
                s, c = (True, ca) if cr["solved"] else (sr["solved"], ca + sa)
            elif label == "probe-then-route":
                s, c = (sr["solved"], p + sa) if route_oof[i] >= thr else (cr["solved"], p + ca)
            else:  # probe-route-then-retry: probe picks the first model, escalate if it fails
                if route_oof[i] >= thr:
                    s, c = sr["solved"], p + sa          # already on the strong model
                elif cr["solved"]:
                    s, c = True, p + ca
                else:
                    s, c = sr["solved"], p + ca + sa
            succ.append(s)
            cost.append(c)
        sm = float(np.mean(succ))
        cm = float(np.mean(cost))
        out[label] = dict(success=sm, spend=cm, total=cm + F * (1 - sm))
    return out


base_cheap_att = float(np.mean([r["attempt"]["cost"] for r in cheap_rows]))
print(f"\nmeasured: cheap attempt ${base_cheap_att:.5f}, probe ${probe_mean:.5f}, "
      f"strong attempt ${np.mean([by_mt[(STRONG,r['task'])]['attempt']['cost'] for r in cheap_rows]):.5f}")

print(f"\npolicies at k=1 with no failure cost (F=0):")
d0 = policy_at(1, 0.0, chow)
for lab in POLICY_MODES:
    print(f"   {lab:<26}success={d0[lab]['success']:.3f}  spend=${d0[lab]['spend']:.5f}")

# sweep the per-failure cost F, expressed as a multiple of one cheap attempt
print(f"\n{'F (x cheap attempt)':<22}" + "".join(f"{l[:20]:<22}" for l in POLICY_MODES) + "winner")
F_cross = {}
for mult in (0, 1, 5, 10, 20, 50, 100, 500):
    F = mult * base_cheap_att
    d = policy_at(1, F, chow)
    w = min(POLICY_MODES, key=lambda x: d[x]["total"])
    print(f"{mult:<22}" + "".join(f"${d[l]['total']:<21.5f}" for l in POLICY_MODES) + w)

# find the F at which each probing policy becomes the outright best
grid_F = np.logspace(-6, -0.5, 400)
for probe_pol in ("probe-then-route", "probe-route-then-retry"):
    hit = None
    for F in grid_F:
        d = policy_at(1, F, chow)
        if min(POLICY_MODES, key=lambda x: d[x]["total"]) == probe_pol:
            hit = F
            break
    F_cross[probe_pol] = hit
    if hit:
        print(f"\n{probe_pol} becomes the best policy once a failure costs >= ${hit:.5f} "
              f"({hit/base_cheap_att:.1f}x one cheap attempt)")
    else:
        print(f"\n{probe_pol} is never the outright best policy at k=1 for any failure cost")

# and as a function of task size k, holding F at the Chow assumption (20x cheap attempt)
print(f"\nwith F fixed at the Chow assumption (20x a cheap attempt), sweeping task size k:")
print(f"{'k':<8}" + "".join(f"{l[:20]:<22}" for l in POLICY_MODES) + "winner")
ks = np.logspace(-0.5, 2.5, 61)
sweep = []
for k in ks:
    d = policy_at(k, 20 * base_cheap_att * k, chow)
    sweep.append((k, d))
for k in (1, 2, 5, 10, 30, 100):
    d = policy_at(k, 20 * base_cheap_att * k, chow)
    w = min(POLICY_MODES, key=lambda x: d[x]["total"])
    print(f"{k:<8}" + "".join(f"${d[l]['total']:<21.5f}" for l in POLICY_MODES) + w)

# latency: probing also buys wall-clock, not just dollars
pl = float(np.mean([r["probe"]["latency_max_s"] for r in cheap_rows]))
cl = float(np.mean([r["attempt"]["latency_s"] for r in cheap_rows]))
sl = float(np.mean([by_mt[(STRONG, r["task"])]["attempt"]["latency_s"] for r in cheap_rows]))
retry_lat = float(np.mean([by_mt[(CHEAP, r["task"])]["attempt"]["latency_s"] +
                           (0 if by_mt[(CHEAP, r["task"])]["solved"]
                            else by_mt[(STRONG, r["task"])]["attempt"]["latency_s"])
                           for r in cheap_rows]))
route_lat = float(np.mean([pl + (sl if route_oof[i] >= chow else cl)
                           for i in range(len(cheap_rows))]))
print(f"\nlatency (mean s/task): probe(parallel)={pl:.1f}  cheap attempt={cl:.1f}  "
      f"strong attempt={sl:.1f}")
print(f"  always-cheap={cl:.1f}   cheap-then-escalate={retry_lat:.1f}   "
      f"probe-then-route={route_lat:.1f}")

# --------------------------------------------------------------------------
# 7. label noise (replicate run)
# --------------------------------------------------------------------------
NOISE = None
if os.path.exists("replicate.jsonl"):
    rep = [json.loads(l) for l in open("replicate.jsonl")]
    flips = sum(1 for r in rep if r["solved_1"] != r["solved_2"])
    NOISE = dict(n=len(rep), flips=flips, flip_rate=flips / len(rep))
    print("\n" + "-" * 78)
    print("7. LABEL NOISE (attempt re-run at identical settings)")
    print("-" * 78)
    print(f"  {flips}/{len(rep)} outcomes flipped = {100*flips/len(rep):.1f}% label noise")
    print("  This bounds the AUROC any predictor could achieve; a perfect predictor of the")
    print("  *distribution* still mislabels flipped cells.")
    for r in rep:
        if r["solved_1"] != r["solved_2"]:
            print(f"    FLIP {r['model']:<18}{r['task']:<24}{r['solved_1']} -> {r['solved_2']}")

# --------------------------------------------------------------------------
# plots
# --------------------------------------------------------------------------
plt.rcParams.update({"figure.dpi": 130, "font.size": 8.5, "axes.grid": True,
                     "grid.alpha": 0.25, "axes.axisbelow": True})

# (f) break-even sweep over the cost of a failure
COLS = {"always-cheap": "#888", "always-strong": "#9a6fb0",
        "cheap-then-escalate": "#2aa198", "probe-then-route": "#3b7dd8",
        "probe-route-then-retry": "#d08770"}
fig, (a1, a2) = plt.subplots(1, 2, figsize=(10.4, 4.4))
Fs = np.logspace(-6, -0.5, 160)
for lab in POLICY_MODES:
    a1.plot(Fs / base_cheap_att, [policy_at(1, F, chow)[lab]["total"] for F in Fs],
            label=lab, color=COLS[lab], lw=1.7)
a1.set_xscale("log")
a1.set_yscale("log")
a1.set_xlabel("cost of one unsolved task, F  (multiples of one cheap attempt)")
a1.set_ylabel("total expected $ per task")
a1.set_title("Probing pays only when failure is expensive")
for pol_name, ls in (("probe-then-route", "--"), ("probe-route-then-retry", ":")):
    if F_cross.get(pol_name):
        a1.axvline(F_cross[pol_name] / base_cheap_att, color=COLS[pol_name], ls=ls, lw=1.2)
a1.legend(fontsize=7)

for lab in POLICY_MODES:
    a2.plot(ks, [d[lab]["total"] for _, d in sweep], label=lab, color=COLS[lab], lw=1.7)
a2.set_xscale("log")
a2.set_yscale("log")
a2.set_xlabel("k = attempt-cost multiplier (k=1 = this suite)")
a2.set_ylabel("total expected $ per task")
a2.set_title("Same comparison as jobs get bigger (F = 20x cheap attempt)")
a2.axvline(1.0, color="k", ls=":", lw=1)
a2.legend(fontsize=7)
fig.tight_layout()
fig.savefig(f"{OUT}/breakeven.png")
plt.close(fig)

# (a) signal AUROC bar chart
fig, ax = plt.subplots(figsize=(8.2, 5.0))
st = [s for s in signal_table if s["family"] != "features" and not math.isnan(s["auroc"])]
st.sort(key=lambda d: d["auroc"])
cols = {"self_consistency": "#3b7dd8", "sketch": "#2aa198", "vconf": "#d08770", "logprob": "#9a6fb0"}
ypos = np.arange(len(st))
ax.barh(ypos, [s["auroc"] for s in st], color=[cols[s["family"]] for s in st],
        xerr=[[max(0, s["auroc"] - s["ci_lo"]) for s in st],
              [max(0, s["ci_hi"] - s["auroc"]) for s in st]],
        error_kw=dict(lw=0.8, ecolor="#555", capsize=2), height=0.68)
ax.axvline(0.5, color="k", lw=1, ls="--")
ax.set_yticks(ypos)
ax.set_yticklabels([f"{s['signal']}  (n={s['coverage']})" for s in st])
ax.set_xlabel("AUROC for predicting FAILURE (0.5 = useless)")
ax.set_xlim(0, 1)
ax.set_title("Pre-flight probe signals: predictive power (pooled, 95% bootstrap CI)")
handles = [plt.Rectangle((0, 0), 1, 1, color=v) for v in cols.values()]
ax.legend(handles, cols.keys(), loc="lower right", fontsize=7.5)
fig.tight_layout()
fig.savefig(f"{OUT}/signal_auroc.png")
plt.close(fig)

# (b) AUROC per dollar
fig, ax = plt.subplots(figsize=(7.6, 4.2))
st2 = [s for s in st if s["cost_per_probe"] > 0]
st2.sort(key=lambda d: d["lift_per_dollar"])
ax.barh(np.arange(len(st2)), [s["lift_per_dollar"] for s in st2],
        color=[cols[s["family"]] for s in st2], height=0.66)
ax.set_yticks(np.arange(len(st2)))
ax.set_yticklabels([s["signal"] for s in st2])
ax.set_xlabel("(AUROC - 0.5) per dollar of probe spend")
ax.set_title("Signal value per dollar")
fig.tight_layout()
fig.savefig(f"{OUT}/signal_auroc_per_dollar.png")
plt.close(fig)

# (c) reliability diagram
fig, ax = plt.subplots(figsize=(5.2, 5.0))
ax.plot([0, 1], [0, 1], "k--", lw=1, label="perfect calibration")
xs = [p for p, o, n in rel_clf]
os_ = [o for p, o, n in rel_clf]
ns = [n for p, o, n in rel_clf]
ax.plot(xs, os_, "o-", color="#3b7dd8", label=f"classifier ({BEST_KEY[0]}/{BEST_KEY[1]})")
for x, o, n in zip(xs, os_, ns):
    ax.annotate(f"n={n}", (x, o), textcoords="offset points", xytext=(5, -9), fontsize=7)
xv = [p for p, o, n in rel_vc]
ov = [o for p, o, n in rel_vc]
nv = [n for p, o, n in rel_vc]
ax.plot(xv, ov, "s-", color="#d08770", label="verbalized confidence")
for x, o, n in zip(xv, ov, nv):
    ax.annotate(f"n={n}", (x, o), textcoords="offset points", xytext=(5, 6), fontsize=7)
ax.set_xlabel("predicted P(failure)")
ax.set_ylabel("observed failure rate")
ax.set_xlim(-0.02, 1.02)
ax.set_ylim(-0.02, 1.02)
ax.set_title("Reliability diagram")
ax.legend(fontsize=7.5, loc="upper left")
fig.tight_layout()
fig.savefig(f"{OUT}/reliability.png")
plt.close(fig)

# (d) policy comparison
fig, (a1, a2) = plt.subplots(1, 2, figsize=(9.6, 4.2))
labels = list(pol)
short = [l.replace("probe-then-route", "probe-route").replace(" (upper bound)", "")
         for l in labels]
sv = [pol[l]["success"] for l in labels]
cvv = [pol[l]["mean_cost"] for l in labels]
c2 = ["#888", "#9a6fb0", "#2aa198", "#3b7dd8", "#5f9ed1", "#4caf50"]
a1.bar(range(len(labels)), sv, color=c2)
a1.set_xticks(range(len(labels)))
a1.set_xticklabels(short, rotation=28, ha="right", fontsize=7)
a1.set_ylabel("success rate")
a1.set_ylim(0, 1.05)
a1.set_title("Task success by policy")
for i, v in enumerate(sv):
    a1.text(i, v + 0.015, f"{v:.2f}", ha="center", fontsize=7)
a2.bar(range(len(labels)), cvv, color=c2)
a2.set_xticks(range(len(labels)))
a2.set_xticklabels(short, rotation=28, ha="right", fontsize=7)
a2.set_ylabel("mean $ per task")
a2.set_title("Cost by policy (probe overhead included)")
for i, v in enumerate(cvv):
    a2.text(i, v * 1.02, f"${v:.4f}", ha="center", fontsize=6.5)
fig.tight_layout()
fig.savefig(f"{OUT}/policy_comparison.png")
plt.close(fig)

# (e) classifier comparison
fig, ax = plt.subplots(figsize=(6.8, 4.0))
kinds = ["features", "probes", "combined", "combined+model"]
w = 0.36
for i, mn in enumerate(("logreg", "gbt")):
    vals = [RESULTS[(k, mn)]["auroc"] for k in kinds]
    errs = [[max(0, RESULTS[(k, mn)]["auroc"] - RESULTS[(k, mn)]["ci"][0]) for k in kinds],
            [max(0, RESULTS[(k, mn)]["ci"][1] - RESULTS[(k, mn)]["auroc"]) for k in kinds]]
    ax.bar(np.arange(len(kinds)) + (i - 0.5) * w, vals, w, label=mn,
           yerr=errs, error_kw=dict(lw=0.8, capsize=2, ecolor="#444"),
           color=["#3b7dd8", "#d08770"][i])
ax.axhline(0.5, color="k", ls="--", lw=1)
ax.set_xticks(np.arange(len(kinds)))
ax.set_xticklabels(kinds, fontsize=8)
ax.set_ylabel("AUROC (leave-one-task-out CV)")
ax.set_ylim(0, 1)
ax.set_title("Failure prediction: feature sets x classifier")
ax.legend()
fig.tight_layout()
fig.savefig(f"{OUT}/classifier_comparison.png")
plt.close(fig)

# --------------------------------------------------------------------------
# dump
# --------------------------------------------------------------------------
out = {
    "n_rows": len(ROWS), "models": MODELS, "n_tasks": len(TASKS),
    "failure_base_rate": float(np.mean(y_all)),
    "signal_table": signal_table,
    "per_model_auroc": {f"{k[0]}|{k[1]}": (None if math.isnan(v) else v)
                        for k, v in per_model_auroc.items()},
    "classifiers": {f"{k[0]}|{k[1]}": dict(auroc=v["auroc"], ci=v["ci"], brier=v["brier"],
                                           acc=v["acc"]) for k, v in RESULTS.items()},
    "best_classifier": f"{BEST_KEY[0]}|{BEST_KEY[1]}",
    "op_95_solvable_retained": best95,
    "chow": dict(threshold=chow, c_escalate=C_ESC, c_fail=C_FAIL, c_attempt=C_ATT, **ochow),
    "calibration": dict(clf_ece=ece(best_oof, y_all), clf_brier=RESULTS[BEST_KEY]["brier"],
                        vconf_ece=ece(vc_fail_pred, y_all[mv]),
                        vconf_brier=float(np.mean((vc_fail_pred - y_all[mv]) ** 2)),
                        mean_stated_conf=float(np.mean(vc[mv])),
                        actual_success=float(1 - np.mean(y_all[mv]))),
    "policies": pol,
    "probe_overhead_pct_of_attempt": {m: 100 * float(np.mean([r["probe"]["cost"] for r in ROWS if r["model"] == m]))
                                      / float(np.mean([r["attempt"]["cost"] for r in ROWS if r["model"] == m]))
                                      for m in MODELS},
    "cheap_model": CHEAP, "strong_model": STRONG,
    "solve_rate": {m: float(solve_rate[m]) for m in MODELS},
    "router_auroc_cheap_only": float(ROUTER_AUROC),
    "router_op_at_chow": ROUTER_OP,
    "label_noise": NOISE,
    "breakeven": {
        "base_cheap_attempt": base_cheap_att,
        "probe_cost": probe_mean,
        "F_cross": {k: (None if v is None else float(v)) for k, v in F_cross.items()},
        "policies_at_F20": {l: policy_at(1, 20 * base_cheap_att, chow)[l] for l in POLICY_MODES},
    },
    "latency_s": {"probe_parallel": pl, "cheap_attempt": cl, "strong_attempt": sl,
                  "always_cheap": cl, "cheap_then_escalate": retry_lat,
                  "probe_then_route": route_lat},
    "truncation": {m: int(sum(1 for r in ROWS if r["model"] == m
                              and r["attempt"]["finish_reason"] == "length")) for m in MODELS},
}
with open("analysis.json", "w") as fh:
    json.dump(out, fh, indent=2, default=str)
print(f"\nwrote analysis.json and {OUT}/*.png")

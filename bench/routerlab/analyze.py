#!/usr/bin/env python3
"""Fit the IRT models, run the separation checks, test price as a cold-start
prior, and simulate routing policies offline.

Nothing here calls an API. Every number comes out of results.jsonl.

Writes: analysis.json, pareto.png, icc.png, and a printed report.
"""
import json
import math
import os
from collections import defaultdict

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
import numpy as np

import irt
import tasks as T

HERE = os.path.dirname(os.path.abspath(__file__))


# ---------------------------------------------------------------------------
# data
# ---------------------------------------------------------------------------

def load():
    recs = []
    with open(os.path.join(HERE, "results.jsonl")) as f:
        for line in f:
            recs.append(json.loads(line))
    panel = json.load(open(os.path.join(HERE, "panel.json")))["panel"]
    return recs, panel


def outcome_obs(recs):
    """The observation list the IRT fit consumes. Replicates enter as separate
    Bernoulli observations of the same cell, which is exactly what the model
    says they are."""
    return [(r["model"], r["task"], 1 if r["ok"] else 0)
            for r in recs if r["final"]]


def informative(obs):
    """Items that are neither all-pass nor all-fail. An item every model passes
    contributes a constant to the likelihood no matter what its difficulty is:
    its ML difficulty runs off to -inf and it tells the scale nothing. The same
    is true in reverse for an item nobody passes. They are reported and then
    excluded from the fit."""
    by = defaultdict(list)
    for m, t, y in obs:
        by[t].append(y)
    keep, allpass, allfail = set(), [], []
    for t, ys in by.items():
        if all(ys):
            allpass.append(t)
        elif not any(ys):
            allfail.append(t)
        else:
            keep.add(t)
    return keep, sorted(allpass), sorted(allfail)


# ---------------------------------------------------------------------------
# cost / latency model for cells the design never ran
# ---------------------------------------------------------------------------

def cost_latency_model(recs, panel):
    """Mean prompt/completion tokens and latency per (model, work-class), from
    the cells that WERE run. The incomplete design leaves ~30% of cells
    unmeasured; a policy that would have routed to one of them needs a cost and
    a latency, and the model's own average on that class of work is the least
    assumption-laden estimate available."""
    price = {m["slug"]: m for m in panel}
    agg = defaultdict(lambda: [0, 0, 0.0, 0])
    for r in recs:
        if not r["final"] or r["error"]:
            continue
        a = agg[(r["model"], r["cls"])]
        a[0] += r["prompt_tokens"]
        a[1] += r["completion_tokens"]
        a[2] += r["latency_s"]
        a[3] += 1
    cost, lat = {}, {}
    for (slug, cls), (pt, ct, ls, n) in agg.items():
        p = price[slug]
        cost[(slug, cls)] = (pt / n / 1e6 * p["price_in_per_mtok"]
                             + ct / n / 1e6 * p["price_out_per_mtok"])
        lat[(slug, cls)] = ls / n
    # fall back to the model's overall mean when a (model, class) pair is empty
    for slug in price:
        vs = [v for (s, c), v in cost.items() if s == slug]
        ls = [v for (s, c), v in lat.items() if s == slug]
        for cls in ("plan", "code", "reason"):
            cost.setdefault((slug, cls), sum(vs) / len(vs) if vs else 0.0)
            lat.setdefault((slug, cls), sum(ls) / len(ls) if ls else 0.0)
    return cost, lat


# ---------------------------------------------------------------------------
# policy simulation
# ---------------------------------------------------------------------------

class Sim:
    """Offline policy simulator.

    A policy is a function task -> ordered list of models to try, with the
    deterministic grader as the verifier: run the first rung, and escalate only
    on a real, graded failure. Expected success, cost and latency of such a
    chain have closed forms, so no Monte Carlo is needed.

    Every policy is scored under two probability sources, and both are reported:

      HYBRID (headline) -- the measured outcome wherever the design actually ran
        that cell, and the fitted 1PL probability only for the ~30% of cells the
        incomplete design skipped. This is the most faithful number available:
        it never invents an outcome it could have looked up.

      PREDICTED -- the fitted probability everywhere, including cells that were
        measured. Reported as the cross-check. Where the two disagree, the model
        is doing the disagreeing, and the disagreement is itself a finding: the
        1PL compresses the easy end, because the 19 items every model passed
        carry no information and so are excluded from the ability fit.
    """

    def __init__(self, panel, prob, cost, lat, tasks):
        self.panel = panel
        self.slugs = [m["slug"] for m in panel]
        self.prob = prob            # (slug, task) -> P(success)
        self.cost = cost            # (slug, cls) -> USD
        self.lat = lat              # (slug, cls) -> seconds
        self.tasks = tasks
        self.cls = {t: T.BY_ID[t]["cls"] for t in tasks}

    def c(self, s, t):
        return self.cost[(s, self.cls[t])]

    def l(self, s, t):
        return self.lat[(s, self.cls[t])]

    def eval_chain(self, chain_for_task):
        succ = cost = lat = 0.0
        for t in self.tasks:
            reach = 1.0    # probability we are still escalating
            s = c = l = 0.0
            for slug in chain_for_task(t):
                p = self.prob[(slug, t)]
                c += reach * self.c(slug, t)
                l += reach * self.l(slug, t)
                s += reach * p
                reach *= (1 - p)
            succ += s
            cost += c
            lat += l
        n = len(self.tasks)
        return succ / n, cost / n, lat / n

    def eval_oracle(self):
        """The omniscient bound: a router that already knows which models will
        succeed sends the task straight to the CHEAPEST one that will, paying
        for exactly one call. Its success rate is the probability that at least
        one panel member succeeds, so it is the ceiling every real policy is
        measured against."""
        succ = cost = lat = 0.0
        for t in self.tasks:
            order = sorted(self.slugs, key=lambda s: self.c(s, t))
            reach, c, l = 1.0, 0.0, 0.0
            for s in order:
                p = self.prob[(s, t)]
                c += reach * p * self.c(s, t)
                l += reach * p * self.l(s, t)
                reach *= (1 - p)
            succ += 1.0 - reach
            # if nobody would have succeeded, the router still pays for a call
            cost += c + reach * self.c(order[0], t)
            lat += l + reach * self.l(order[0], t)
        n = len(self.tasks)
        return succ / n, cost / n, lat / n


def main():
    recs, panel = load()
    slugs = [m["slug"] for m in panel]
    label = {m["slug"]: m["label"] for m in panel}
    obs_all = outcome_obs(recs)
    keep, allpass, allfail = informative(obs_all)
    obs = [o for o in obs_all if o[1] in keep]

    print("=" * 78)
    print("OUTCOME MATRIX")
    print("=" * 78)
    print(f"cells in final matrix : {len(obs_all)}")
    print(f"overall pass rate     : {sum(o[2] for o in obs_all)/len(obs_all):.4f}")
    print(f"informative items     : {len(keep)} of 36")
    print(f"all-pass (excluded)   : {len(allpass)}  {allpass}")
    print(f"all-fail (excluded)   : {len(allfail)}  {allfail}")
    print(f"cells entering the fit: {len(obs)}")

    # ---------------- IRT fits ----------------
    f1 = irt.fit(obs, "1PL")
    f2 = irt.fit(obs, "2PL")
    stat, df, pval = irt.lr_test(f1, f2)

    th_se = f1.theta_se()
    order = np.argsort(-f1.theta)
    print()
    print("=" * 78)
    print("RASCH (1PL) ABILITY")
    print("=" * 78)
    print(f"{'model':<20}{'theta':>9}{'SE':>7}{'raw pass':>10}{'$/Mout':>9}")
    raw = defaultdict(lambda: [0, 0])
    for m, t, y in obs_all:
        raw[m][0] += y
        raw[m][1] += 1
    price = {m["slug"]: m for m in panel}
    for i in order:
        m = f1.models[i]
        k, n = raw[m]
        print(f"{label[m]:<20}{f1.theta[i]:>9.3f}{th_se[i]:>7.3f}"
              f"{k/n:>9.3f} {price[m]['price_out_per_mtok']:>8.3f}")

    # pairwise separation on invariant differences
    print()
    print("pairwise theta differences (invariant to identification)")
    print(f"{'pair':<34}{'diff':>8}{'SE':>7}{'z':>7}{'p(1-sided)':>12}")
    sep_rows = []
    for a in range(len(order)):
        for bb in range(a + 1, len(order)):
            i, j = order[a], order[bb]
            d = f1.theta[i] - f1.theta[j]
            se = f1.diff_se(i, j)
            z = d / se if se > 0 else float("inf")
            p = irt.normal_sf(abs(z))
            sep_rows.append((label[f1.models[i]], label[f1.models[j]], d, se, z, p))
    for r in sep_rows:
        star = "*" if r[5] < 0.05 else " "
        print(f"{r[0]+' > '+r[1]:<34}{r[2]:>8.3f}{r[3]:>7.3f}{r[4]:>7.2f}"
              f"{r[5]:>11.4f} {star}")
    nsig = sum(1 for r in sep_rows if r[5] < 0.05)

    rel, G, sd_true, rmse = irt.separation_reliability(f1.theta, th_se)
    print(f"\nseparation reliability {rel:.3f}   separation index G = {G:.2f}"
          f"   (true SD {sd_true:.3f}, RMSE {rmse:.3f})")
    print(f"significant pairwise orderings: {nsig} of {len(sep_rows)}")

    # ---------------- difficulties ----------------
    b_se = f1.b_se()
    print()
    print("=" * 78)
    print("ITEM DIFFICULTY (1PL), informative items")
    print("=" * 78)
    print(f"{'item':<7}{'cls':<8}{'lvl':>4}{'b':>8}{'SE':>7}{'pass':>7}"
          f"{'infit':>8}{'outfit':>8}")
    resid = irt.residual_fit_stats(f1, obs)
    emp = defaultdict(lambda: [0, 0])
    for m, t, y in obs:
        emp[t][0] += y
        emp[t][1] += 1
    bo = np.argsort(-f1.b)
    misfit = []
    for i in bo:
        t = f1.items[i]
        k, n = emp[t]
        inf, outf, _ = resid[t]
        flag = ""
        if outf > 1.5 or inf > 1.5:
            flag = "  <- noisy"
            misfit.append((t, inf, outf))
        elif outf < 0.5:
            flag = "  <- overfit/deterministic"
            misfit.append((t, inf, outf))
        print(f"{t:<7}{T.BY_ID[t]['cls']:<8}{T.BY_ID[t]['level']:>4}"
              f"{f1.b[i]:>8.3f}{b_se[i]:>7.3f}{k/n:>7.2f}{inf:>8.2f}{outf:>8.2f}{flag}")

    # ---------------- 1PL vs 2PL ----------------
    print()
    print("=" * 78)
    print("1PL vs 2PL")
    print("=" * 78)
    print(f"{'model':<8}{'logLik':>11}{'k':>5}{'AIC':>10}{'BIC':>10}")
    print(f"{'1PL':<8}{f1.loglik:>11.3f}{f1.n_params:>5}{f1.aic:>10.2f}{f1.bic:>10.2f}")
    print(f"{'2PL':<8}{f2.loglik:>11.3f}{f2.n_params:>5}{f2.aic:>10.2f}{f2.bic:>10.2f}")
    print(f"LR statistic {stat:.3f} on {df} df, p = {pval:.4f}")
    verdict_2pl = ("2PL" if (pval < 0.05 and f2.aic < f1.aic) else "1PL")
    print(f"verdict: keep {verdict_2pl}"
          + ("" if verdict_2pl == "2PL" else
             " -- the extra discrimination parameters do not pay for themselves"))
    print("2PL discriminations a_t:")
    for i in np.argsort(-f2.a):
        print(f"   {f2.items[i]:<7}{f2.a[i]:>7.3f}")

    # ---------------- price as a cold-start prior ----------------
    print()
    print("=" * 78)
    print("PRICE AS A COLD-START PRIOR FOR THETA")
    print("=" * 78)
    th = np.array([f1.theta[f1.models.index(s)] for s in slugs])
    logp = np.log(np.array([price[s]["price_out_per_mtok"] for s in slugs]))
    logc = np.log(np.array([price[s]["context_length"] for s in slugs]))

    def pearson(x, y):
        x, y = np.asarray(x, float), np.asarray(y, float)
        xc, yc = x - x.mean(), y - y.mean()
        d = math.sqrt(float(xc @ xc) * float(yc @ yc))
        return float(xc @ yc) / d if d > 0 else float("nan")

    def spearman(x, y):
        rx = np.argsort(np.argsort(x)).astype(float)
        ry = np.argsort(np.argsort(y)).astype(float)
        return pearson(rx, ry)

    n = len(slugs)

    def rp(r):
        if abs(r) >= 1:
            return 0.0
        tstat = r * math.sqrt((n - 2) / (1 - r * r))
        # two-sided p from a t with n-2 df, via the chi-square/normal helpers
        # is not available; use the normal approximation and say so.
        return 2 * irt.normal_sf(abs(tstat))

    r_price, s_price = pearson(logp, th), spearman(logp, th)
    r_ctx, s_ctx = pearson(logc, th), spearman(logc, th)
    print(f"theta vs log(output price):  pearson r = {r_price:+.3f} "
          f"(p~{rp(r_price):.3f}), spearman rho = {s_price:+.3f}")
    print(f"theta vs log(context len):   pearson r = {r_ctx:+.3f} "
          f"(p~{rp(r_ctx):.3f}), spearman rho = {s_ctx:+.3f}")
    print("(n = 7 models; p-values are normal approximations and are indicative only)")
    for s in sorted(slugs, key=lambda s: price[s]["price_out_per_mtok"]):
        i = f1.models.index(s)
        print(f"   {label[s]:<20} ${price[s]['price_out_per_mtok']:>6.3f}/Mout"
              f"   theta {f1.theta[i]:+.3f}")

    # ---------------- probabilities for every cell ----------------
    theta_by_model = {m: f1.theta[i] for i, m in enumerate(f1.models)}
    # Difficulties for ALL 36 items, including the 19 nobody failed and the one
    # nobody passed. Those have no finite ML difficulty, so the same weak prior
    # used in the fit keeps them on the scale; they are needed only so the
    # policy simulation has a probability for every cell.
    b_all = irt.fit_difficulty_only(obs_all, theta_by_model,
                                    prior_sd=irt.PRIOR_SD)
    task_ids = sorted(T.BY_ID)
    phat = {(s, t): float(irt.sigmoid(theta_by_model[s] - b_all[t]))
            for s in slugs for t in task_ids}

    measured = {}
    mm = defaultdict(lambda: [0, 0])
    for r in recs:
        if r["final"]:
            mm[(r["model"], r["task"])][0] += 1 if r["ok"] else 0
            mm[(r["model"], r["task"])][1] += 1
    for k, (a, b) in mm.items():
        measured[k] = a / b
    # hybrid: look the answer up when it was measured, predict only when it
    # was not
    pmix = {k: measured.get(k, phat[k]) for k in phat}
    n_meas = sum(1 for k in phat if k in measured)

    cost, lat = cost_latency_model(recs, panel)
    sim = Sim(panel, pmix, cost, lat, task_ids)
    sim_pred = Sim(panel, phat, cost, lat, task_ids)

    # ---------------- calibration of the fitted probabilities ------------
    zs, ys = [], []
    for m, t, y in obs_all:
        zs.append(theta_by_model[m] - b_all[t])
        ys.append(y)
    zs, ys = np.array(zs), np.array(ys)
    bins = np.quantile(zs, np.linspace(0, 1, 7))
    bins[-1] += 1e-9
    icc_x, icc_y, icc_n = [], [], []
    for i in range(len(bins) - 1):
        sel = (zs >= bins[i]) & (zs < bins[i + 1])
        if sel.sum() >= 3:
            icc_x.append(float(zs[sel].mean()))
            icc_y.append(float(ys[sel].mean()))
            icc_n.append(int(sel.sum()))
    pred = irt.sigmoid(zs)
    brier = float(np.mean((pred - ys) ** 2))
    base = float(np.mean((ys.mean() - ys) ** 2))
    print()
    print("=" * 78)
    print("ITEM CHARACTERISTIC CURVE CHECK")
    print("=" * 78)
    print(f"{'theta-b bin mean':>18}{'empirical':>12}{'model':>9}{'n':>6}")
    for x, y, nn in zip(icc_x, icc_y, icc_n):
        print(f"{x:>18.3f}{y:>12.3f}{float(irt.sigmoid(x)):>9.3f}{nn:>6}")
    print(f"Brier score {brier:.4f} vs {base:.4f} for a constant base rate "
          f"(skill {1 - brier/base:+.3f})")

    # ---------------- policies ----------------
    price_out = {s: price[s]["price_out_per_mtok"] for s in slugs}
    by_cost = sorted(slugs, key=lambda s: price_out[s])

    def single(s):
        return lambda t: [s]

    # Every routing decision is made from the FITTED probabilities only. A
    # router that peeked at the measured outcome would be an oracle, not a
    # policy; the measured outcomes are used to score the decision, never to
    # make it.
    def route_irt(threshold=0.8):
        def f(t):
            for s in sorted(slugs, key=lambda s: sim.c(s, t)):
                if phat[(s, t)] >= threshold:
                    return [s]
            return [max(slugs, key=lambda s: phat[(s, t)])]
        return f

    def cascade(t):
        """Rungs ordered by predicted success per dollar; the deterministic
        grader is the verifier, so escalation happens only on a real failure."""
        return sorted(slugs, key=lambda s: -phat[(s, t)] / max(sim.c(s, t), 1e-9))

    def route_cascade(threshold=0.8):
        def f(t):
            ch = cascade(t)
            for i, s in enumerate(ch):
                if phat[(s, t)] >= threshold:
                    return ch[i:]
            return ch
        return f

    policies = []
    for s in by_cost:
        policies.append((f"single: {label[s]}", single(s)))
    policies.append(("route-by-IRT (p>=0.8)", route_irt(0.8)))
    policies.append(("cascade (p/cost order)", cascade))
    policies.append(("route+cascade", route_cascade(0.8)))

    print()
    print("=" * 78)
    print("POLICY SIMULATION  (36 tasks)")
    print("=" * 78)
    print(f"measured cells available to the hybrid scorer: {n_meas} of "
          f"{len(slugs) * len(task_ids)} ({n_meas / (len(slugs) * len(task_ids)):.0%})")
    print(f"{'policy':<26}{'succ':>7}{'$/task':>10}{'lat s':>8}"
          f"{'  | pred succ':>14}{'pred $':>9}")
    rows = []
    for name, fn in policies:
        s, c, l = sim.eval_chain(fn)
        ps, pc, pl = sim_pred.eval_chain(fn)
        rows.append({"policy": name, "success": s, "cost_per_task": c,
                     "latency_s": l, "pred_success": ps, "pred_cost": pc})
        print(f"{name:<26}{s:>7.3f}{c:>10.5f}{l:>8.2f}"
              f"{ps:>14.3f}{pc:>9.5f}")
    os_, oc, ol = sim.eval_oracle()
    ops, opc, opl = sim_pred.eval_oracle()
    rows.append({"policy": "oracle best-per-task", "success": os_,
                 "cost_per_task": oc, "latency_s": ol,
                 "pred_success": ops, "pred_cost": opc})
    print(f"{'oracle best-per-task':<26}{os_:>7.3f}{oc:>10.5f}{ol:>8.2f}"
          f"{ops:>14.3f}{opc:>9.5f}")

    # Pareto frontier over IMPLEMENTABLE policies. The oracle is excluded and
    # reported separately: it needs the answer before choosing the model, so
    # putting it on the frontier would only hide the policies that are actually
    # deployable behind a bound nobody can reach.
    impl = [r for r in rows if r["policy"] != "oracle best-per-task"]
    pts = sorted(impl, key=lambda r: (r["cost_per_task"], -r["success"]))
    frontier, best = [], -1
    for r in pts:
        if r["success"] > best + 1e-12:
            frontier.append(r["policy"])
            best = r["success"]
    print("\nPareto frontier over implementable policies "
          "(cost-increasing, success-improving):")
    for p in frontier:
        r = next(x for x in impl if x["policy"] == p)
        print(f"   {p:<26} {r['success']:.3f} @ ${r['cost_per_task']:.6f}/task")
    orow = next(r for r in rows if r["policy"] == "oracle best-per-task")
    print(f"   [bound] oracle           {orow['success']:.3f} @ "
          f"${orow['cost_per_task']:.6f}/task -- not implementable")

    # What the winning cascade actually does -- the composition matters more
    # for Phase B than the headline number, because it says which models have
    # to be wired up and how often a second call happens.
    first = defaultdict(int)
    exp_calls = 0.0
    for t in task_ids:
        ch = cascade(t)
        first[label[ch[0]]] += 1
        reach = 1.0
        for s in ch:
            exp_calls += reach
            reach *= (1 - pmix[(s, t)])
    print("\ncascade composition")
    print(f"   expected calls per task: {exp_calls / len(task_ids):.2f}")
    print("   first rung chosen, by task count:")
    for k, v in sorted(first.items(), key=lambda kv: -kv[1]):
        print(f"      {k:<20}{v:>3} / {len(task_ids)}")

    default = "~deepseek/deepseek-v4-flash-latest"
    drow = next(r for r in rows if r["policy"] == f"single: {label[default]}")
    print(f"\nincumbent default = {label[default]}: "
          f"success {drow['success']:.3f}, ${drow['cost_per_task']:.5f}/task")
    for r in rows:
        if r["policy"].startswith("single"):
            continue
        ds = r["success"] - drow["success"]
        dc = (r["cost_per_task"] / drow["cost_per_task"] - 1) * 100
        print(f"   {r['policy']:<26} success {ds:+.3f}   cost {dc:+.1f}%")

    # ---------------- figures ----------------
    make_pareto(rows, label, os.path.join(HERE, "pareto.png"))
    make_icc(icc_x, icc_y, icc_n, f1, label, os.path.join(HERE, "icc.png"))

    spend_total = sum(r["cost_usd"] for r in recs)
    out = {
        "matrix": {
            "cells_final": len(obs_all),
            "calls_total": len(recs),
            "pass_rate": sum(o[2] for o in obs_all) / len(obs_all),
            "informative_items": sorted(keep),
            "all_pass_items": allpass,
            "all_fail_items": allfail,
        },
        "spend_usd_total": spend_total,
        "irt_1pl": {
            "theta": {label[m]: float(f1.theta[i]) for i, m in enumerate(f1.models)},
            "theta_se": {label[m]: float(th_se[i]) for i, m in enumerate(f1.models)},
            "difficulty": {t: float(f1.b[i]) for i, t in enumerate(f1.items)},
            "difficulty_se": {t: float(b_se[i]) for i, t in enumerate(f1.items)},
            "loglik": f1.loglik, "aic": f1.aic, "bic": f1.bic,
            "n_params": f1.n_params,
        },
        "irt_2pl": {
            "loglik": f2.loglik, "aic": f2.aic, "bic": f2.bic,
            "n_params": f2.n_params,
            "discrimination": {t: float(f2.a[i]) for i, t in enumerate(f2.items)},
        },
        "lr_test": {"stat": stat, "df": df, "p": pval, "verdict": verdict_2pl},
        "separation": {
            "reliability": rel, "G": G, "true_sd": sd_true, "rmse": rmse,
            "significant_pairs": nsig, "total_pairs": len(sep_rows),
            "pairs": [{"hi": a, "lo": b, "diff": d, "se": se, "z": z, "p": p}
                      for a, b, d, se, z, p in sep_rows],
        },
        "calibration": {"brier": brier, "base_brier": base,
                        "icc_bins": [{"z": x, "empirical": y, "n": nn}
                                     for x, y, nn in zip(icc_x, icc_y, icc_n)]},
        "misfit_items": [{"item": t, "infit": i, "outfit": o} for t, i, o in misfit],
        "price_prior": {
            "pearson_theta_logprice": r_price,
            "spearman_theta_logprice": s_price,
            "pearson_theta_logcontext": r_ctx,
            "spearman_theta_logcontext": s_ctx,
        },
        "policies": rows,
        "pareto_frontier": frontier,
    }
    with open(os.path.join(HERE, "analysis.json"), "w") as f:
        json.dump(out, f, indent=2)
    print(f"\nwrote analysis.json, pareto.png, icc.png")
    print(f"total API spend across both rounds: ${spend_total:.4f}")


def make_pareto(rows, label, path):
    fig, ax = plt.subplots(figsize=(9, 6))
    singles = [r for r in rows if r["policy"].startswith("single")]
    routed = [r for r in rows
              if not r["policy"].startswith("single")
              and r["policy"] != "oracle best-per-task"]
    orac = [r for r in rows if r["policy"] == "oracle best-per-task"]

    # Labels are nudged apart by hand: cascade and route+cascade land within a
    # few percent of each other and the default offset stacks their text.
    nudge = {"cascade (p/cost order)": (-22, 13),
             "route+cascade": (10, -16),
             "route-by-IRT (p>=0.8)": (10, -4),
             "oracle best-per-task": (12, 2)}

    def scat(rs, color, marker, tag):
        x = [r["cost_per_task"] * 1000 for r in rs]
        y = [r["success"] for r in rs]
        s = [max(r["latency_s"], 0.1) * 45 for r in rs]
        ax.scatter(x, y, s=s, c=color, marker=marker, alpha=0.72,
                   edgecolors="black", linewidths=0.7, label=tag, zorder=3)
        for r in rs:
            name = r["policy"].replace("single: ", "")
            ax.annotate(name, (r["cost_per_task"] * 1000, r["success"]),
                        textcoords="offset points",
                        xytext=nudge.get(r["policy"], (9, 5)), fontsize=8)

    scat(singles, "#7a9cc6", "o", "single model")
    scat(routed, "#d1603d", "D", "routed / cascade")
    scat(orac, "#4c9a6a", "*", "oracle bound (not implementable)")

    pts = sorted(singles + routed, key=lambda r: (r["cost_per_task"], -r["success"]))
    fx, fy, best = [], [], -1
    for r in pts:
        if r["success"] > best + 1e-12:
            fx.append(r["cost_per_task"] * 1000)
            fy.append(r["success"])
            best = r["success"]
    ax.step(fx, fy, where="post", color="#444", lw=1.2, ls="--", alpha=0.8,
            zorder=2, label="Pareto frontier")

    ax.set_xscale("log")
    ax.set_ylim(top=1.03)
    ax.set_xlabel("mean cost per task (US$ x 1000, log scale)")
    ax.set_ylabel("success rate over the 36-task suite\n"
                  "(measured where the design ran the cell, fitted elsewhere)")
    ax.set_title("Cost-quality frontier: single models vs routed panels\n"
                 "(marker area proportional to mean latency)", fontsize=11)
    ax.grid(alpha=0.25, which="both")
    ax.legend(loc="lower right", fontsize=9)
    fig.tight_layout()
    fig.savefig(path, dpi=150)
    plt.close(fig)


def make_icc(icc_x, icc_y, icc_n, f1, label, path):
    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(11, 4.6))
    xs = np.linspace(-6, 6, 300)
    ax1.plot(xs, irt.sigmoid(xs), color="#333", lw=2,
             label="Rasch ICC  P = sigmoid(theta - b)")
    ax1.scatter(icc_x, icc_y, s=[max(n, 4) * 4 for n in icc_n], color="#d1603d",
                edgecolors="black", linewidths=0.6, zorder=3,
                label="empirical pass rate (equal-count bins)")
    ax1.set_xlabel("theta - b  (ability minus difficulty, logits)")
    ax1.set_ylabel("P(pass)")
    ax1.set_title("Do fitted difficulties predict the fail->pass flip?",
                  fontsize=10)
    ax1.grid(alpha=0.25)
    ax1.legend(fontsize=8, loc="upper left")

    order = np.argsort(f1.theta)
    ax2.barh([label[f1.models[i]] for i in order], [f1.theta[i] for i in order],
             xerr=[f1.theta_se()[i] for i in order], color="#7a9cc6",
             edgecolor="black", linewidth=0.6, height=0.6)
    ax2.axvline(0, color="#666", lw=0.8)
    ax2.set_xlabel("theta (logits, mean item difficulty = 0)")
    ax2.set_title("Fitted ability with standard errors", fontsize=10)
    ax2.grid(alpha=0.25, axis="x")
    fig.tight_layout()
    fig.savefig(path, dpi=150)
    plt.close(fig)


if __name__ == "__main__":
    main()

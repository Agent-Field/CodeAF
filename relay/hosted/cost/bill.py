#!/usr/bin/env python3
"""Turn measured billed phase usage into monthly dollars per user, and render section 9 of STAGE-1H-DECISION.md.

Arithmetic (every number comes from the run, the SHAPE block below, or model.PRICE):
  background  The idle_hold phase is a steady state: one home screen open, one lease held, idle heartbeats.
              Its billed usage divided by its seconds is a per-second rate; times
              home_hours_per_day * 3600 * days_per_month seconds it is the background month.
  warm move   warm_moves phase usage divided by its count.
  cold move   cold_moves phase usage divided by its count.
  month       background + warm_per_day * days * warm + cold_per_day * days * cold.
  The result is a usage vector per user-month with the same keys as a billed phase.
Dollars: the per-user vector times N users, then charged with model.py's tier arithmetic (`over`, PRICE):
  Workers base once, Worker requests and CPU ms, DO requests, DO GB-s, DO rows, R2 Class A and B.
  Rows read and rows written are summed and charged at the one DO row price PRICE has, which over-charges reads.
  No storage line is billed here; storage is in model.py's common().
Marginal dollars (the per-user column) use no free tier and no Workers base, the cost of one more user.
"""
import argparse
import json
import re
import sys

from model import PRICE, over

# One place for the assumed heavy-user shape; the manifest's "shape" and the command line override it.
SHAPE = dict(home_hours_per_day=8, warm_per_day=40, cold_per_day=4, days_per_month=22)
SCENARIOS = [(10_000, 0.25), (10_000, 1.0), (100_000, 0.25), (100_000, 1.0)]
KEYS = ["worker_requests", "worker_cpu_ms", "do_requests", "do_active_s", "do_gb_s",
        "do_rows_read", "do_rows_written", "r2_class_a", "r2_class_b"]
USAGE_ROWS = [("Worker requests", "worker_requests"), ("Worker CPU ms", "worker_cpu_ms"),
              ("DO requests", "do_requests"), ("DO GB-s", "do_gb_s"),
              ("DO rows read", "do_rows_read"), ("DO rows written", "do_rows_written"),
              ("R2 Class A", "r2_class_a"), ("R2 Class B", "r2_class_b")]
PENDING = "n/a (pending)"
HEADING = "## 9. Billed heavy-user run"


def scale(vec, k):
    return {key: vec.get(key, 0) * k for key in KEYS}


def add(*vecs):
    return {key: sum(v.get(key, 0) for v in vecs) for key in KEYS}


def month_usage(manifest, billed, shape):
    """Per-user month as a usage vector, from the phase windows and the billed usage of each phase."""
    phases = {p["name"]: p for p in manifest["phases"]}
    used = billed["phases"]
    days = shape["days_per_month"]
    background_s = shape["home_hours_per_day"] * 3600 * days
    background = scale(used["idle_hold"], background_s / phases["idle_hold"]["seconds"])
    warm = scale(used["warm_moves"], shape["warm_per_day"] * days / phases["warm_moves"]["count"])
    cold = scale(used["cold_moves"], shape["cold_per_day"] * days / phases["cold_moves"]["count"])
    return add(background, warm, cold)


def charge(u, free, base):
    """Dollars for one usage vector; a `free` of 0 and `base` of 0 gives the marginal cost."""
    p = PRICE
    tier = lambda key, used, per_m: over(used, free * p[key], per_m)
    rows = u["do_rows_read"] + u["do_rows_written"]
    return (base * p["workers_base"]
            + tier("req_free", u["worker_requests"], p["req_per_m"])
            + tier("cpu_free_ms", u["worker_cpu_ms"], p["cpu_per_m_ms"])
            + tier("do_req_free", u["do_requests"], p["do_req_per_m"])
            + tier("do_gbs_free", u["do_gb_s"], p["do_gbs_per_m"])
            + tier("do_rows_free", rows, p["do_rows_per_m"])
            + tier("r2_a_free", u["r2_class_a"], p["r2_a_per_m"])
            + tier("r2_b_free", u["r2_class_b"], p["r2_b_per_m"]))


def dollars(user_month):
    """Marginal per user-month, then each scenario with free tiers and the Workers base applied."""
    out = [("Per user-month (marginal, no free tier)", charge(user_month, 0, 0))]
    for n, active in SCENARIOS:
        out.append((f"{n:,} users, {active:.0%} active", charge(scale(user_month, n * active), 1, 1)))
    return out


def num(x):
    return f"{x:,.0f}" if abs(x) >= 100 else f"{x:,.2f}"


def table(head, rows):
    lines = [f"| {head} | before (this run) | after |", "|---|---:|---:|"]
    return lines + [f"| {name} | {b} | {a} |" for name, b, a in rows]


def pair(before, after, fmt):
    """Rows of (label, before, after) text; after is the placeholder when there is no after run."""
    return [(label, fmt(b), fmt(after[i][1]) if after else PENDING) for i, (label, b) in enumerate(before)]


def usage_rows(before, after):
    return [(label, num(before[k]), num(after[k]) if after else PENDING) for label, k in USAGE_ROWS]


def ratio_line(before, after):
    parts = [f"{label} {after[k] / before[k]:.2f}" for label, k in (("DO GB-s", "do_gb_s"), ("DO requests", "do_requests"))
             if before[k]]
    return "Ratio after/before per user-month: " + ", ".join(parts) + "." if parts else ""


def intro(manifest, billed, shape):
    s = shape
    return (f"Script `{billed['script']}`, window {manifest['start']} to {manifest['end']}. Shape per user: home screen open "
            f"{s['home_hours_per_day']} h/day, {s['warm_per_day']} warm moves and {s['cold_per_day']} cold moves per day, "
            f"{s['days_per_month']} days/month. The idle_hold phase rate per second times the open-home seconds is the "
            "background month; each move type adds its phase cost per move times its monthly count. Dollars charge the "
            "usage times N users with the free tiers and Workers base in model.py; storage is not billed here.")


def render(manifest, billed, shape, after=None):
    before_m = month_usage(manifest, billed, shape)
    after_m = month_usage(*after[:2], after[2]) if after else None
    bd = dollars(before_m)
    ad = dollars(after_m) if after_m else None
    lines = [HEADING, "", intro(manifest, billed, shape), "", "Usage per user-month.", ""]
    lines += table("Usage", usage_rows(before_m, after_m))
    lines += ["", "Dollars per month.", ""]
    lines += table("Scenario", pair(bd, ad, lambda d: f"${d:,.2f}" if d < 100 else f"${d:,.0f}"))
    if after_m:
        lines += ["", ratio_line(before_m, after_m)]
    return "\n".join(lines) + "\n"


def splice(doc, section):
    """Replace section 9 (heading to the next '## ' heading or the end) or append it; no other byte changes."""
    m = re.search(r"^## 9\. Billed heavy-user run[^\n]*\n", doc, re.M)
    if not m:
        return doc.rstrip("\n") + "\n\n" + section if doc else section
    nxt = re.compile(r"^## ", re.M).search(doc, m.end())
    end = nxt.start() if nxt else len(doc)
    old = doc[m.start():end]
    tail = old[len(old.rstrip("\n")):] or "\n"
    return doc[:m.start()] + section.rstrip("\n") + tail + doc[end:]


def load(path):
    with open(path) as f:
        return json.load(f)


def shape_for(manifest, overrides):
    return {**SHAPE, **manifest.get("shape", {}), **{k: v for k, v in overrides.items() if v is not None}}


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--manifest", required=True)
    ap.add_argument("--billed", required=True)
    ap.add_argument("--after-manifest")
    ap.add_argument("--after-billed")
    ap.add_argument("--doc")
    for key in SHAPE:
        ap.add_argument("--" + key.replace("_", "-"), dest=key, type=float)
    a = ap.parse_args(argv)
    over_ = {k: getattr(a, k) for k in SHAPE}
    manifest = load(a.manifest)
    after = None
    if a.after_manifest and a.after_billed:
        am = load(a.after_manifest)
        after = (am, load(a.after_billed), shape_for(am, over_))
    section = render(manifest, load(a.billed), shape_for(manifest, over_), after)
    if not a.doc:
        sys.stdout.write(section)
        return
    with open(a.doc) as f:
        doc = f.read()
    with open(a.doc, "w") as f:
        f.write(splice(doc, section))


if __name__ == "__main__":
    main()

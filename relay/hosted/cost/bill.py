#!/usr/bin/env python3
"""Turn measured billed phase usage into monthly dollars per user, and render section 9 of STAGE-1H-DECISION.md.

Arithmetic (every number comes from the run, the SHAPE block below, or model.PRICE):
  background  The idle_hold phase is a steady state: one home screen open, one lease held, idle heartbeats.
              Its billed usage divided by its seconds is a per-second rate; times
              home_hours_per_day * 3600 * days_per_month seconds it is the background month.
  warm move   warm_moves phase usage divided by its count.
  cold move   cold_moves phase usage divided by its count.
  month       background + warm_per_month * warm + cold_per_month * cold.
  The result is a usage vector per user-month with the same keys as a billed phase.
Dollars: the per-user vector times N users, then charged with model.py's tier arithmetic (`over`, PRICE):
  Workers base once, Worker requests and CPU ms, DO requests, DO GB-s, DO rows, R2 Class A and B.
  Rows read and rows written are separate meters (ROWS_READ, ROWS_WRITTEN).
  No storage line is billed here; storage is in model.py's common().
Marginal dollars (the per-user column) use no free tier and no Workers base, the cost of one more user.
"""
import argparse
import json
import re
import sys

from model import PRICE, over

# One place for the assumed heavy-user shape; the manifest's "shape" and the command line override it.
# The plan's heavy user: a home screen open 8 h on each of 22 working days, and 40 warm plus 4 cold moves in the MONTH.
SHAPE = dict(home_hours_per_day=8, days_per_month=22, warm_per_month=40, cold_per_month=4)
# Durable Object SQLite rows, from developers.cloudflare.com/durable-objects/platform/pricing (fetched 2026-09-30):
# reads are a thousand times cheaper than writes, so they are two meters and not the one PRICE has.
ROWS_READ = dict(free=25e9, per_m=0.001)
ROWS_WRITTEN = dict(free=50e6, per_m=1.0)
SCENARIOS = [(10_000, 0.25), (10_000, 1.0), (100_000, 0.25), (100_000, 1.0)]
KEYS = ["worker_requests", "worker_cpu_ms", "do_requests", "do_active_s", "do_gb_s",
        "do_rows_read", "do_rows_written", "r2_class_a", "r2_class_b"]
USAGE_ROWS = [("Worker requests", "worker_requests"), ("Worker CPU ms", "worker_cpu_ms"),
              ("DO requests", "do_requests"), ("DO GB-s", "do_gb_s"),
              ("DO rows read", "do_rows_read"), ("DO rows written", "do_rows_written"),
              ("R2 Class A", "r2_class_a"), ("R2 Class B", "r2_class_b")]
PENDING = "n/a (pending)"
HEARTBEAT_S = 30  # directory.HeartbeatEvery: an idle held lease sends one directory write this often
HEADING = "## 9. Billed heavy-user run"


def scale(vec, k):
    return {key: vec.get(key, 0) * k for key in KEYS}


def add(*vecs):
    return {key: sum(v.get(key, 0) for v in vecs) for key in KEYS}


def class_a_floor(used, phase):
    """A phase's billed usage with R2 Class A raised to the frame puts the client sent, each a PutObject."""
    puts = phase.get("client_requests", {}).get("POST /v1/store/frames", 0)
    return {**used, "r2_class_a": max(used.get("r2_class_a", 0), puts)}


def month_usage(manifest, billed, shape):
    """Per-user month as a usage vector, from the phase windows and the billed usage of each phase."""
    phases = {p["name"]: p for p in manifest["phases"]}
    used = {n: class_a_floor(v, phases[n]) for n, v in billed["phases"].items()}
    days = shape["days_per_month"]
    background_s = shape["home_hours_per_day"] * 3600 * days
    background = scale(used["idle_hold"], background_s / phases["idle_hold"]["seconds"])
    warm = scale(used["warm_moves"], shape["warm_per_month"] / phases["warm_moves"]["count"])
    cold = scale(used["cold_moves"], shape["cold_per_month"] / phases["cold_moves"]["count"])
    return add(background, warm, cold)


def meters(u, free, base):
    """One row per priced meter: (name, quantity, price text, dollars). free=0 and base=0 is the marginal cost."""
    p = PRICE
    def meter(name, key, qty, free_qty, per_m, unit):
        return (name, qty, f"${per_m:g} per million {unit} after {free_qty * free:,.0f}", over(qty, free_qty * free, per_m))
    return [
        ("Workers base", base, "$5 a month", base * p["workers_base"]),
        meter("Worker requests", "req", u["worker_requests"], p["req_free"], p["req_per_m"], "requests"),
        meter("Worker CPU ms", "cpu", u["worker_cpu_ms"], p["cpu_free_ms"], p["cpu_per_m_ms"], "ms"),
        meter("DO requests", "doreq", u["do_requests"], p["do_req_free"], p["do_req_per_m"], "requests"),
        meter("DO GB-s", "gbs", u["do_gb_s"], p["do_gbs_free"], p["do_gbs_per_m"], "GB-s"),
        meter("DO rows read", "rr", u["do_rows_read"], ROWS_READ["free"], ROWS_READ["per_m"], "rows"),
        meter("DO rows written", "rw", u["do_rows_written"], ROWS_WRITTEN["free"], ROWS_WRITTEN["per_m"], "rows"),
        meter("R2 Class A", "a", u["r2_class_a"], p["r2_a_free"], p["r2_a_per_m"], "ops"),
        meter("R2 Class B", "b", u["r2_class_b"], p["r2_b_free"], p["r2_b_per_m"], "ops"),
    ]


def charge(u, free, base):
    """Dollars for one usage vector."""
    return sum(m[3] for m in meters(u, free, base))


def breakdown(user_month, n, active, title):
    """The markdown table of every meter for one scenario, so its total can be added by eye."""
    rows = meters(scale(user_month, n * active), 1, 1)
    lines = [f"Dollar breakdown, {title}: {n:,} users, {active:.0%} active (free tiers and Workers base applied).", "",
             "| Meter | Monthly quantity | Price | Dollars |", "|---|---:|---|---:|"]
    lines += [f"| {name} | {num(q)} | {price} | ${d:,.2f} |" for name, q, price, d in rows]
    return lines + [f"| **Total** | | | **${sum(r[3] for r in rows):,.2f}** |"]


def dollars(user_month):
    """Marginal per user-month, then each scenario with free tiers and the Workers base applied."""
    out = [("Per user-month (marginal, no free tier)", charge(user_month, 0, 0))]
    for n, active in SCENARIOS:
        out.append((f"{n:,} users, {active:.0%} active", charge(scale(user_month, n * active), 1, 1)))
    return out


def num(x):
    return f"{x:,.0f}" if abs(x) >= 100 else f"{x:,.2f}"


def table(head, titles, rows):
    """A markdown table with one column per run; rows are (name, [cell per run])."""
    lines = ["| " + " | ".join([head] + titles) + " |", "|---|" + "---:|" * len(titles)]
    return lines + ["| " + " | ".join([name] + cells) + " |" for name, cells in rows]


def usage_rows(months):
    return [(label, [num(m[k]) for m in months]) for label, k in USAGE_ROWS]


def dollar_rows(months):
    all_dollars = [dollars(m) for m in months]
    return [(all_dollars[0][i][0], [f"${d[i][1]:,.2f}" if d[i][1] < 100 else f"${d[i][1]:,.0f}" for d in all_dollars])
            for i in range(len(all_dollars[0]))]


def ratio_lines(titles, months):
    """Each later column against the first: the two meters the push work is meant to move."""
    first, out = months[0], []
    for title, m in zip(titles[1:], months[1:]):
        parts = [f"{label} {m[k] / first[k]:.2f}" for label, k in (("DO GB-s", "do_gb_s"), ("DO requests", "do_requests"),
                                                                    ("DO rows written", "do_rows_written")) if first[k]]
        out.append(f"Ratio {title} / {titles[0]} per user-month: " + ", ".join(parts) + ".")
    return out


def rows_sentence(manifest, billed):
    """Which verb writes the rows: the idle hold sends only heartbeats and list reads, and only heartbeats write."""
    idle = next(p for p in manifest["phases"] if p["name"] == "idle_hold")
    beats = idle["seconds"] / HEARTBEAT_S
    written = billed["phases"]["idle_hold"]["do_rows_written"]
    return (f"Rows written are the largest line because a held lease heartbeats every {HEARTBEAT_S} s for the whole time the "
            f"home is open: in the idle hold (about {beats:.0f} heartbeats, nothing else writes) the relay wrote {written:,.0f} "
            f"rows, {written / beats:.1f} per heartbeat. In the Worker (`directory.js`) a heartbeat runs one `INSERT OR REPLACE INTO dir`, whose measured cost fits a replace "
            "counted as a delete and an insert, and the `UPDATE dirver` of the durable version only when the list-visible record changed, "
            "which a beat that moves the expiry alone does not. Rows read come from the list, which selects every directory row.")


def intro(columns, shape):
    """The shared paragraph: the shape, the arithmetic, then each run's window and notes."""
    s = shape
    text = (f"Shape per user: home screen open {s['home_hours_per_day']} h/day on {s['days_per_month']:g} days a month, and "
            f"{s['warm_per_month']:g} warm plus {s['cold_per_month']:g} cold moves in the month. The idle_hold phase rate per "
            "second times the open-home seconds is the background month; each move type adds its phase cost per move times its "
            "monthly count. Dollars charge the usage times N users with the free tiers and Workers base in model.py; storage is "
            "not billed here. R2 Class A is the larger of the billed count and the frame puts the client sent, because the "
            "analytics were seen to miss puts (12 frame puts, 0 billed).")
    lines = [text, "", rows_sentence(columns[0]["manifest"], columns[0]["billed"]), ""]
    for c in columns:
        m = c["manifest"]
        notes = " ".join([m.get("note", "")] + [p["note"].capitalize() + "." for p in m["phases"] if p.get("note")]).strip()
        lines.append(f"- **{c['title']}**: `{c['billed']['script']}`, window {m['start']} to {m['end']}. {notes}".rstrip())
    return "\n".join(lines)


def render(columns, shape):
    """Section 9 for any number of runs, each a dict of title, manifest and billed."""
    months = [month_usage(c["manifest"], c["billed"], shape) for c in columns]
    titles = [c["title"] for c in columns]
    lines = [HEADING, "", intro(columns, shape), "", "Usage per user-month.", ""]
    lines += table("Usage", titles, usage_rows(months))
    lines += ["", "Dollars per month.", ""]
    lines += table("Scenario", titles, dollar_rows(months))
    for title, m in zip(titles, months):
        lines += [""] + breakdown(m, 10_000, 1.0, title)
    lines += [""] + ratio_lines(titles, months)
    return "\n".join(lines).rstrip() + "\n"


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
    ap.add_argument("--column", nargs=3, action="append", required=True, metavar=("TITLE", "MANIFEST", "BILLED"),
                    help="one run, in the order the columns are shown; repeat for each run")
    ap.add_argument("--doc")
    for key in SHAPE:
        ap.add_argument("--" + key.replace("_", "-"), dest=key, type=float)
    a = ap.parse_args(argv)
    columns = [dict(title=t, manifest=load(m), billed=load(b)) for t, m, b in a.column]
    section = render(columns, shape_for(columns[0]["manifest"], {k: getattr(a, k) for k in SHAPE}))
    if not a.doc:
        sys.stdout.write(section)
        return
    with open(a.doc) as f:
        doc = f.read()
    with open(a.doc, "w") as f:
        f.write(splice(doc, section))


if __name__ == "__main__":
    main()

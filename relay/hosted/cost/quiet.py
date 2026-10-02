#!/usr/bin/env python3
"""Re-read a steady phase from only its quiet minutes, when another session shared the staging Worker.

The billed analytics are per Worker, so traffic from anyone else lands in the phase. A steady phase
(the idle hold) is one client doing almost nothing, so a minute with more than --max-requests Worker
requests cannot be that client alone. This keeps only the minutes at or under the threshold (a minute
with no data is quiet) and writes a manifest and a billed file whose phase is exactly those minutes, so
the per-second rate that bill.py extrapolates is taken over clean time only. The other phases are copied.

  python3 quiet.py --manifest run.json --billed billed.json --out-prefix PATH [--max-requests 3]
"""
import argparse
import json
from datetime import timedelta

import billed

MINUTE = timedelta(minutes=1)


def worker_minutes(client, script, lo, hi):
    q = ('workersInvocationsAdaptive(limit:1000, filter:{scriptName:"%s", datetime_geq:"%s", datetime_leq:"%s"}) '
         '{ dimensions { datetimeMinute } sum { requests } }' % (script, billed.iso(lo), billed.iso(hi - timedelta(seconds=1))))
    rows = client.run(q)["workersInvocationsAdaptive"]
    return {billed.parse_time(r["dimensions"]["datetimeMinute"]): r["sum"]["requests"] for r in rows}


def quiet_minutes(counts, lo, hi, limit):
    out, m = [], lo
    while m < hi:
        if counts.get(m, 0) <= limit:
            out.append(m)
        m += MINUTE
    return out


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--manifest", required=True)
    ap.add_argument("--billed", required=True)
    ap.add_argument("--out-prefix", required=True)
    ap.add_argument("--max-requests", type=int, default=3)
    ap.add_argument("--bucket", default=billed.DEFAULT_BUCKET)
    ap.add_argument("--account", default=billed.DEFAULT_ACCOUNT)
    a = ap.parse_args(argv)
    manifest, result = json.load(open(a.manifest)), json.load(open(a.billed))
    client = billed.Client(a.account)
    phase = next(p for p in manifest["phases"] if p.get("steady"))
    lo, hi = billed.minute_window(billed.parse_time(phase["start"]), billed.parse_time(phase["end"]))
    keep = quiet_minutes(worker_minutes(client, manifest["script"] if "script" in manifest else result["script"], lo, hi), lo, hi, a.max_requests)
    script = result["script"]
    namespaces = billed.discover_namespaces(client, script, lo, hi)
    reads = [billed.read_phase(client, script, a.bucket, namespaces, m, m + MINUTE, (m, m)) for m in keep]
    summed = billed.sum_phases(dict(enumerate(reads)))
    result["phases"][phase["name"]] = summed
    result["total"] = billed.sum_phases(result["phases"])
    phase["seconds"] = 60.0 * len(keep)
    phase["note"] = (f"read from the {len(keep)} quiet minutes of {int((hi - lo) / MINUTE)} (at most {a.max_requests} Worker requests "
                     "a minute) because another session was loading the staging Worker in the rest")
    json.dump(manifest, open(a.out_prefix + ".run.json", "w"), indent=2)
    json.dump(result, open(a.out_prefix + ".billed.json", "w"), indent=2)
    print(f"kept {len(keep)} minutes: " + " ".join(m.strftime("%H:%M") for m in keep))
    print(json.dumps(summed))


if __name__ == "__main__":
    main()

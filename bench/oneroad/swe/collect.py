#!/usr/bin/env python3
"""collect.py — the SWE track's rows as one CSV."""
import csv, glob, json, os, sys
OUT_DEFAULT = os.environ.get("SWE_OUT", "/home/santosh/af-bench/swe")
FIELDS = ["task", "harness", "seed", "outcome", "settle_reason", "verdict", "reward",
          "tests_passed", "tests_failed", "changed_files", "agent_patch_bytes",
          "road", "route", "marks", "division", "division_why", "parts", "peak_workers",
          "forks", "calls", "cost_usd", "cost_by_role", "endpoint_mix",
          "wall_s", "verify_wall_s", "validation_primary", "model"]

def main():
    root = sys.argv[1] if len(sys.argv) > 1 else OUT_DEFAULT
    rows = []
    for p in sorted(glob.glob(os.path.join(root, "*", "meta.json"))):
        rows.append(json.load(open(p)))
    rows.sort(key=lambda r: (str(r.get("task")), str(r.get("harness"))))
    out = os.path.join(root, "results.csv")
    with open(out, "w", newline="") as fh:
        w = csv.DictWriter(fh, fieldnames=FIELDS, extrasaction="ignore",
                           quoting=csv.QUOTE_MINIMAL)
        w.writeheader()
        for r in rows:
            w.writerow(r)
    print(f"{len(rows)} cells -> {out}")

if __name__ == "__main__":
    main()

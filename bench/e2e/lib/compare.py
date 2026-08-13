#!/usr/bin/env python3
"""compare.py — the regression verdict at a glance.

Reads bench-results/e2e.csv and answers the two questions the battery exists
for: did this run move against the last one, and how does it sit against the
peer harness on the same task.

Nothing here recomputes anything. Every number is a number some cell already
measured and wrote down; this only subtracts them and says which way.

Invoked as `bench/e2e/run.sh compare`.
"""

import csv
import sys
from collections import defaultdict

# A move smaller than these is noise, not a regression.
#
# The two numbers are different because the two quantities are. Cost is a sum of
# billed tokens and moves only when the run does more work: two back-to-back
# runs of the lookup cell came in at $0.001992 and $0.0022, about 10% apart.
# Wall clock also carries queueing, provider latency and whatever else the
# machine is doing, and the same two runs took 15s and 24s — a 60% "regression"
# between two runs of identical code.
#
# So the wall threshold is set above that observed spread deliberately. A
# battery that reports a regression every time it is run twice teaches its
# reader to ignore it, and then it is guarding nothing. Real wall regressions in
# this engine's history have been multiples — the abcc5a5 window-sizing defect
# was 3.2x — and those clear any threshold in this range.
WALL_NOISE_PCT = 75.0
COST_NOISE_PCT = 25.0


def load(path):
    try:
        with open(path, newline="", encoding="utf-8") as handle:
            return [row for row in csv.DictReader(handle) if row.get("cell")]
    except FileNotFoundError:
        sys.exit(f"no results yet at {path} — run bench/e2e/run.sh first")


def number(value):
    try:
        return float(value)
    except (TypeError, ValueError):
        return None


def delta_pct(new, old):
    """Percent change from old to new, or None when it cannot be computed."""
    if new is None or old is None or old == 0:
        return None
    return (new - old) / old * 100.0


def arrow(pct, noise):
    if pct is None:
        return "    —  "
    if abs(pct) < noise:
        return f" {pct:+5.0f}%  "
    return f" {pct:+5.0f}% {'▲' if pct > 0 else '▼'}"


def show(value, width, suffix=""):
    return f"{value}{suffix}".rjust(width) if value is not None else "—".rjust(width)


def main():
    path = sys.argv[1] if len(sys.argv) > 1 else "bench-results/e2e.csv"
    rows = load(path)

    # Rows arrive in run order and are never rewritten, so "latest" is simply
    # the last one for a (cell, harness) and "previous" the one before it.
    history = defaultdict(list)
    for row in rows:
        history[(row["cell"], row.get("harness", "aforge"))].append(row)

    cells = []
    for cell, harness in history:
        if cell not in cells:
            cells.append(cell)

    print(f"e2e regression report — {path}")
    print()
    print("aforge, latest run vs the one before it")
    print(f"  {'cell':<10} {'wall':>8} {'Δwall':>9} {'cost':>9} {'Δcost':>9} {'nodes':>6} {'route':>11}  quality")
    print("  " + "─" * 82)

    regressions = []
    for cell in cells:
        runs = history.get((cell, "aforge"), [])
        if not runs:
            continue
        latest = runs[-1]
        previous = runs[-2] if len(runs) > 1 else None

        wall = number(latest.get("wall_s"))
        cost = number(latest.get("cost_usd"))
        quality = latest.get("quality_pass", "?")

        d_wall = d_cost = None
        flip = ""
        if previous:
            d_wall = delta_pct(wall, number(previous.get("wall_s")))
            d_cost = delta_pct(cost, number(previous.get("cost_usd")))
            was = previous.get("quality_pass", "?")
            if was != quality:
                flip = f"  {was} → {quality}"
                if quality == "fail":
                    regressions.append(f"{cell}: quality {was} → {quality}")
            if d_wall is not None and d_wall > WALL_NOISE_PCT:
                regressions.append(f"{cell}: wall {d_wall:+.0f}%")
            if d_cost is not None and d_cost > COST_NOISE_PCT:
                regressions.append(f"{cell}: cost {d_cost:+.0f}%")
        if quality == "fail" and not flip:
            regressions.append(f"{cell}: quality still failing")

        print(
            f"  {cell:<10}"
            f" {show(wall, 7, 's')}"
            f" {arrow(d_wall, WALL_NOISE_PCT)}"
            f" {show(cost, 9)}"
            f" {arrow(d_cost, COST_NOISE_PCT)}"
            f" {show(latest.get('nodes') or None, 6)}"
            f" {(latest.get('route') or '—'):>11}"
            f"  {quality}{flip}"
        )

    # The peer comparison. Only rows that actually ran are shown; a skipped arm
    # is reported as skipped rather than omitted, because "pi has no row" and
    # "pi refused to run on a different model" are different facts.
    peers = sorted({harness for _, harness in history if harness != "aforge"})
    for peer in peers:
        print()
        print(f"aforge vs {peer}, latest of each")
        print(f"  {'cell':<10} {'aforge wall':>12} {f'{peer} wall':>12} {'Δ':>9}  quality (aforge / {peer})")
        print("  " + "─" * 78)
        for cell in cells:
            ours = history.get((cell, "aforge"), [])
            theirs = history.get((cell, peer), [])
            if not theirs:
                continue
            latest_peer = theirs[-1]
            our_wall = number(ours[-1].get("wall_s")) if ours else None
            our_quality = ours[-1].get("quality_pass", "—") if ours else "—"
            if latest_peer.get("quality_pass") == "skipped":
                print(
                    f"  {cell:<10}"
                    f" {show(our_wall, 11, 's')}"
                    f" {'skipped':>12} {'—':>9}"
                    f"  {our_quality} / skipped — {latest_peer.get('notes','')[:44]}"
                )
                continue
            their_wall = number(latest_peer.get("wall_s"))
            print(
                f"  {cell:<10}"
                f" {show(our_wall, 11, 's')}"
                f" {show(their_wall, 11, 's')}"
                f" {arrow(delta_pct(their_wall, our_wall), WALL_NOISE_PCT)}"
                f"  {our_quality} / {latest_peer.get('quality_pass','—')}"
            )

    # The model column is the comparability check. Two different models in one
    # report is not a comparison, and it is worth saying loudly rather than
    # leaving for the reader to notice.
    models = defaultdict(set)
    for row in rows:
        if row.get("model") and row.get("quality_pass") != "skipped":
            models[row.get("harness", "aforge")].add(row["model"])
    print()
    print("model pins in this file")
    for harness in sorted(models):
        for model in sorted(models[harness]):
            print(f"  {harness:<10} {model}")
    if any(len(seen) > 1 for seen in models.values()):
        print("  ⚠ a harness has rows on more than one model — those rows are not comparable")

    print()
    if regressions:
        print(
            f"REGRESSIONS ({len(regressions)}) — wall over {WALL_NOISE_PCT:.0f}%, "
            f"cost over {COST_NOISE_PCT:.0f}%, or a quality flip:"
        )
        for item in regressions:
            print(f"  ✗ {item}")
    else:
        print(
            f"no regressions — wall within {WALL_NOISE_PCT:.0f}%, "
            f"cost within {COST_NOISE_PCT:.0f}%, no quality flips"
        )


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""Read a run's cells and report, conservatively, what the data can support.

THE STRATUM IS THE UNIT. Two cells may sit side by side in a file and still not
be comparable: a different door, a different model pin, or a different effort
rung makes them measurements of different things. So every figure below is
computed inside an exact stratum — scenario, door, model, effort_requested,
effort_sent — and never across strata. Workload labels stay on the rows as
description, but no workload score is ever produced by averaging scenarios,
because that would average incomparable things.

SUCCESS IS BINARY. A cell succeeded when its verdict is pass AND every check it
ran passed. A wrong answer that cleared nine infrastructure checks on the way
is a failure, and counting the fraction of checks passed would call it 90%
right. A cell that ran no assertions at all has no success value and is
excluded, because a number cannot be judged right or wrong when nothing asked.

FAILURES STAY IN THE DENOMINATOR. A timeout or a wrong answer is an attempt that
did not succeed, and dropping it would hand every arm the survivor's record. A
cost the harness could not report is likewise kept in view: the missing-cost
count is shown, total and mean cost appear only when every attempt in the cell
is billed, and cost per success divides the cost of ALL attempts by the
successes. Missing wall time is never read as zero — it is counted as missing
and excluded from the wall figures.

PAIRED BLOCKS OR DESCRIPTION, NEVER A LEADERBOARD. When cells carry
experiment_id and block_id, arms are compared only through blocks that contain
exactly one attempt per expected arm under matching condition metadata and a
consistent arm_version per arm; anything else is rejected by name. With five or
more paired blocks the paired differences get a deterministic bootstrap 95%
interval. Point means and all-success samples never establish confidence: equal
small samples cannot prove equivalence, and an observed nondominated arm is
labelled exploratory — a statement about these blocks, never a general optimum.

Legacy files without pairing are reported DESCRIPTIVELY ONLY: the tables say
what was run and what it cost, and nothing in them ranks, dominates, or
declares a frontier, because unpaired data cannot separate an arm difference
from run-to-run noise.
"""
import argparse
import collections
import json
import math
import random
import sys

# The condition metadata that defines a stratum. Cells must agree on all of
# these to be compared; a cell missing any of them cannot even be placed.
STRATUM_FIELDS = ("scenario", "door", "model_pin", "effort_requested", "effort_sent")

# Bootstrap settings. The seed is fixed so two people reading the same file see
# the same intervals; a report whose confidence band changes between runs is a
# report nobody can quote.
BOOT_N = 10000
BOOT_SEED = 20260905

# Below this many attempts the p50/p95 are computed but flagged, because a
# percentile of four numbers carries the shape of those four numbers and
# nothing else.
TINY_N = 10


def load(paths):
    cells = []
    for path in paths:
        with open(path, errors="replace") as handle:
            for line in handle:
                line = line.strip()
                if not line:
                    continue
                try:
                    cells.append(json.loads(line))
                except ValueError:
                    print("skipping a line that is not JSON in %s" % path, file=sys.stderr)
    return cells


def attempt_status(cell):
    """Say whether a row is a real attempt, and why not when it is not.

    Rows the rig writes about itself — unsupported scenarios, skipped arms,
    cells flagged not comparable — are refusals to measure, not measurements,
    and putting them in a denominator would charge an arm for the rig's day."""
    verdict = cell.get("verdict")
    if verdict in ("unsupported", "skipped"):
        return False, "%s: %s" % (verdict, cell.get("reason") or "no reason recorded")
    if cell.get("comparable") != "yes":
        # Old run.sh marks missing billing as noncomparable. It is still a real
        # quality attempt; dropping it would erase failures with lost receipts.
        reasons = [x.strip() for x in (cell.get("reason") or "").split(";") if x.strip()]
        if not reasons or any(x not in {"cost not self-reported", "cost not fully accounted"} for x in reasons):
            return False, "not comparable: %s" % (cell.get("reason") or "no reason recorded")
    if not (cell.get("checks") or []) and verdict not in {"timeout", "crash"}:
        return False, "the cell made no assertions, so success cannot be judged"
    return True, ""


def success(cell):
    """Binary success: the verdict is pass AND every check passed. A fraction of
    passed checks would let a wrong answer with tidy infrastructure outrank a
    correct one, which is exactly the mistake this tool exists to prevent."""
    if cell.get("verdict") != "pass":
        return False
    checks = cell.get("checks") or []
    return bool(checks) and all(check.get("outcome") == "pass" for check in checks)


def wall_of(cell):
    value = cell.get("wall_s")
    if value is None or value == "":
        return None
    try:
        number = float(value)
        return number if not isinstance(value, bool) and math.isfinite(number) and number >= 0 else None
    except (TypeError, ValueError):
        return None


def cost_of(cell):
    value = cell.get("cost_usd")
    if value is None:
        return None
    try:
        number = float(value)
        return number if not isinstance(value, bool) and math.isfinite(number) and number >= 0 else None
    except (TypeError, ValueError):
        return None


def percentile(values, fraction):
    """Nearest-rank percentile. With the tiny samples this suite produces the
    choice of interpolation method is noise, so the boring method wins."""
    if not values:
        return None
    ordered = sorted(values)
    rank = max(1, math.ceil(fraction * len(ordered)))
    return ordered[min(rank, len(ordered)) - 1]


def arm_stats(cells):
    """Aggregate one arm's attempts inside a single stratum. Failures and
    timeouts stay in every denominator; missing cost and missing wall are
    counted instead of guessed."""
    walls = [wall_of(cell) for cell in cells]
    walls_known = [w for w in walls if w is not None]
    costs = [cost_of(cell) for cell in cells]
    costs_known = [c for c in costs if c is not None]
    n_success = sum(1 for cell in cells if success(cell))
    cost_complete = len(costs_known) == len(cells)
    stats = {
        "n_attempted": len(cells),
        "n_success": n_success,
        "missing_cost": len(cells) - len(costs_known),
        "missing_wall": len(cells) - len(walls_known),
        "mean_wall": (sum(walls_known) / len(walls_known)) if walls_known else None,
        "p50_wall": percentile(walls_known, 0.50),
        "p95_wall": percentile(walls_known, 0.95),
        "cost_complete": cost_complete,
        "total_cost": sum(costs_known) if cost_complete else None,
        "mean_cost": (sum(costs_known) / len(costs_known)) if cost_complete else None,
        # Cost per success divides the cost of ALL attempts by the successes,
        # so an arm that fails expensively is not let off by its wins. With no
        # successes it is undefined, not zero — division by zero is not a
        # measurement.
        "cost_per_success": (sum(costs_known) / n_success) if (cost_complete and n_success > 0) else None,
    }
    return stats


def fmt_wall(value):
    return "%.1f" % value if value is not None else "—"


def fmt_cost(value):
    return "%.4f" % value if value is not None else "—"


def print_stats_row(arm, stats):
    print("     %-10s %6d %6d  %11s %6d %6d %9s %9s %9s %11s %10s %10s"
          % (arm, stats["n_attempted"], stats["n_success"],
             "%d/%d" % (stats["n_success"], stats["n_attempted"]),
             stats["missing_cost"], stats["missing_wall"],
             fmt_wall(stats["mean_wall"]), fmt_wall(stats["p50_wall"]),
             fmt_wall(stats["p95_wall"]),
             fmt_cost(stats["total_cost"]), fmt_cost(stats["mean_cost"]),
             fmt_cost(stats["cost_per_success"])))
    if stats["n_attempted"] < TINY_N:
        print("       (tiny n: %d attempt(s); every figure above is exploratory)"
              % stats["n_attempted"])
    if not stats["cost_complete"]:
        print("       (cost withheld: %d attempt(s) have no billing; no total, mean,"
              " or per-success cost is computed from a partial ledger)"
              % stats["missing_cost"])


def stratum_label(stratum):
    return "  ".join("%s=%s" % (name, value)
                     for name, value in zip(STRATUM_FIELDS, stratum))


def stratum_of(cell):
    return tuple(cell.get(field) for field in STRATUM_FIELDS)


def conditions_recorded(cell):
    """A cell whose condition metadata is absent cannot be verified to match
    its blockmates, and an unverified match is not a match."""
    return all(cell.get(field) not in (None, "") for field in STRATUM_FIELDS)


def pair_experiment(experiment_id, stratum, cells, excluded):
    """Compare arms through fully balanced paired blocks, rejecting everything
    that is not exactly one attempt per expected arm under one condition set."""
    by_block = collections.defaultdict(list)
    for cell in cells:
        if not conditions_recorded(cell):
            excluded.append((experiment_id, stratum, cell.get("arm"),
                             "block %s: condition metadata is missing, so a match cannot be verified"
                             % cell.get("block_id")))
            continue
        by_block[cell.get("block_id")].append(cell)

    # A campaign declares expected arms before running. Older paired imports
    # can only use the observed union; they cannot certify a planned competitor
    # which is absent from the entire file.
    declarations = {tuple(sorted(cell["expected_arms"])) for cell in cells if cell.get("expected_arms")}
    if len(declarations) > 1:
        print("inconsistent expected_arms; nothing is compared")
        return
    expected_arms = list(next(iter(declarations))) if declarations else sorted({cell.get("arm") for cell in cells})
    if len(expected_arms) < 2:
        print("\n== experiment %s  (stratum: %s)" % (experiment_id, stratum_label(stratum)))
        print("   fewer than two arms appear; a single arm is not a comparison")
        return

    kept = {}
    rejections = []
    for block_id, block_cells in sorted(by_block.items(), key=lambda item: str(item[0])):
        arms_in_block = [cell.get("arm") for cell in block_cells]
        duplicates = [arm for arm, count in collections.Counter(arms_in_block).items() if count > 1]
        if duplicates:
            rejections.append("block %s: duplicate — %s appears %d time(s)"
                              % (block_id, ", ".join(sorted(map(str, duplicates))),
                                 max(collections.Counter(arms_in_block).values())))
            continue
        missing = [arm for arm in expected_arms if arm not in arms_in_block]
        if missing:
            rejections.append("block %s: unbalanced — no attempt for %s"
                              % (block_id, ", ".join(map(str, missing))))
            continue
        if set(arms_in_block) != set(expected_arms):
            rejections.append("block %s: unexpected arm" % block_id)
            continue
        if any(cell.get("condition_id") is not None for cell in cells):
            conditions = {cell.get("condition_id") for cell in block_cells}
            if None in conditions or len(conditions) != 1:
                rejections.append("block %s: condition_id differs or is missing" % block_id)
                continue
        kept[block_id] = {cell.get("arm"): cell for cell in block_cells}

    # An arm measured at two versions is two different arms wearing one name,
    # and a comparison across them says nothing reproducible about either.
    versions = collections.defaultdict(set)
    for block in kept.values():
        for arm, cell in block.items():
            versions[arm].add((str(cell.get("arm_version")), str(cell.get("arm_binary_sha256"))))
    mixed = {arm: vs for arm, vs in versions.items() if len(vs) > 1}
    condition_versions = {cell.get("condition_id") for block in kept.values() for cell in block.values()}
    if len(condition_versions) > 1:
        rejections.append("condition_id changed across blocks; comparison refused")
        kept = {}
    if mixed:
        for arm, vs in sorted(mixed.items(), key=lambda item: str(item[0])):
            rejections.append("arm %s: arm_version is inconsistent (%s); the whole comparison"
                              " for this experiment and stratum is refused"
                              % (arm, ", ".join(str(v) for v in sorted(vs))))
        kept = {}

    print("\n== experiment %s  (stratum: %s)" % (experiment_id, stratum_label(stratum)))
    print("   expected arms (declared if available, otherwise inferred): %s" % ", ".join(map(str, expected_arms)))
    print("   blocks: %d paired, %d rejected" % (len(kept), len(rejections)))
    for reason in rejections:
        print("   rejected — %s" % reason)
    if not kept:
        print("   no fully balanced paired blocks remain; nothing is compared")
        return

    blocks = [kept[block_id] for block_id in sorted(kept, key=str)]
    print("   per arm, over the paired blocks only:")
    print("     %-10s %6s %6s  %11s %6s %6s %9s %9s %9s %11s %10s %10s"
          % ("arm", "tried", "succ", "success", "miss$$", "miss_s",
             "mean s", "p50 s", "p95 s", "total $", "mean $", "$/succ"))
    stats_by_arm = {}
    for arm in expected_arms:
        cells_for_arm = [block[arm] for block in blocks]
        stats_by_arm[arm] = arm_stats(cells_for_arm)
        print_stats_row(arm, stats_by_arm[arm])

    if len(blocks) >= 5 and len(expected_arms) >= 2:
        print("   paired differences (second arm minus first), 95%% bounds over %d blocks;"
              " exact binomial for success, paired bootstrap for cost/time:" % len(blocks))
        for i in range(len(expected_arms)):
            for j in range(i + 1, len(expected_arms)):
                arm_a, arm_b = expected_arms[i], expected_arms[j]
                pairs = [(block[arm_a], block[arm_b]) for block in blocks]
                print("     %s vs %s:" % (arm_a, arm_b))
                for metric, transform in (
                        ("success", lambda cell: 1.0 if success(cell) else 0.0),
                        ("cost $", cost_of),
                        ("wall s", wall_of)):
                    diffs = []
                    # The diff is computed only where both sides are known; a
                    # pair with one missing value carries no information about
                    # the difference and inventing one would bias the interval.
                    diffs = []
                    for a, b in pairs:
                        va, vb = transform(a), transform(b)
                        if va is not None and vb is not None:
                            diffs.append(vb - va)
                    if len(diffs) != len(pairs):
                        print("       %-8s — incomplete measurements; interval withheld to avoid survivor bias" % metric)
                        continue
                    if metric == "success":
                        # Bootstrap resampling cannot discover unseen failures.
                        # Exact binomial bounds retain uncertainty at 5/5 and
                        # 0/5; two 97.5% marginal intervals give a conservative
                        # 95% bound for their difference by the union bound.
                        a_bounds = binomial_interval(sum(success(a) for a, _ in pairs), len(pairs), 0.025)
                        b_bounds = binomial_interval(sum(success(b) for _, b in pairs), len(pairs), 0.025)
                        interval = (b_bounds[0]-a_bounds[1], b_bounds[1]-a_bounds[0])
                    else:
                        interval = bootstrap_ci(diffs)
                    sign = "+" if interval[0] > 0 else ("−" if interval[1] < 0 else "")
                    print("       %-8s mean %+.4f  95%% CI [%+.4f, %+.4f]  (over %d pair(s))"
                          % (metric, sum(diffs) / len(diffs), interval[0], interval[1],
                             len(diffs)))
                    if metric == "success":
                        print("         conservative exact binomial difference bound; repeated identical fixtures do not establish task diversity")
                    if interval[0] > 0 or interval[1] < 0:
                        print("         the interval excludes zero — evidence about these"
                              " blocks only, not a general claim")
                    elif metric == "success" and \
                            all(success(block[arm_a]) and success(block[arm_b]) for block in blocks):
                        print("         both arms succeeded in every paired block; equal"
                              " all-success samples cannot establish equivalence"
                              " at n=%d" % len(blocks))
    else:
        print("   fewer than 5 paired blocks: no interval is computed, and the"
              " figures above carry no confidence beyond these %d block(s)." % len(blocks))

    if any(not s["cost_complete"] or s["missing_wall"] for s in stats_by_arm.values()):
        print("   incomplete cost/time coverage: observed nondominance is withheld")
        return
    observed = observed_nondominated(expected_arms, stats_by_arm)
    print("   observed, not dominated on these blocks: %s"
          % (", ".join(map(str, sorted(observed, key=str))) or "nothing"))
    print("   exploratory only: this lists what no other arm beat on these blocks;"
          " it is not a frontier, not a ranking, and not a claim about any run"
          " or condition not printed above.")


def binomial_interval(successes, n, alpha=0.05):
    """Invert binomial tails, retaining uncertainty even when every attempt wins."""
    def solve(k, upper_tail, target):
        lo, hi = 0.0, 1.0
        for _ in range(60):
            p = (lo+hi)/2
            indices = range(k, n+1) if upper_tail else range(k+1)
            value = sum(math.comb(n, i)*p**i*(1-p)**(n-i) for i in indices)
            if (value < target) == upper_tail:
                lo = p
            else:
                hi = p
        return (lo+hi)/2
    return (0.0 if successes == 0 else solve(successes, True, alpha/2),
            1.0 if successes == n else solve(successes, False, alpha/2))


def bootstrap_ci(diffs):
    """Percentile bootstrap over paired differences. The pairs are the exchange
    unit — resampling them preserves the block structure that makes the
    comparison honest — and the fixed seed keeps the interval reproducible."""
    rng = random.Random(BOOT_SEED)
    n = len(diffs)
    means = []
    for _ in range(BOOT_N):
        means.append(sum(diffs[rng.randrange(n)] for _ in range(n)) / n)
    means.sort()
    return means[int(0.025 * BOOT_N)], means[int(0.975 * BOOT_N) - 1]


def observed_nondominated(arms, stats_by_arm):
    """Arms no other arm beats on all measured quantities at once, computed
    from paired-block means. It is labelled exploratory everywhere it is
    printed because point means carry no confidence; the bootstrap section
    above is the only place this report speaks with one."""
    def dominates(a, b):
        sa, sb = stats_by_arm[a], stats_by_arm[b]
        if sa["missing_wall"] or sb["missing_wall"] or sa["mean_wall"] is None or sb["mean_wall"] is None:
            return False
        if not sa["cost_complete"] or not sb["cost_complete"]:
            return False
        at_least = (sa["n_success"] / sa["n_attempted"] >= sb["n_success"] / sb["n_attempted"]
                    and sa["mean_wall"] <= sb["mean_wall"])
        strictly = (sa["n_success"] / sa["n_attempted"] > sb["n_success"] / sb["n_attempted"]
                    or sa["mean_wall"] < sb["mean_wall"])
        if sa["cost_complete"] and sb["cost_complete"]:
            at_least = at_least and sa["mean_cost"] <= sb["mean_cost"]
            strictly = strictly or sa["mean_cost"] < sb["mean_cost"]
        return at_least and strictly

    return [arm for arm in arms
            if not any(dominates(other, arm) for other in arms if other != arm)]


def print_legacy(stratum, by_arm, excluded):
    """Describe a stratum from files with no pairing. The word 'descriptive' is
    the whole contract: unpaired runs cannot separate an arm difference from
    run-to-run noise, so this table never ranks, dominates, or declares a
    frontier — it says what was run and what each attempt cost."""
    print("\n== descriptive summary  (stratum: %s)" % stratum_label(stratum))
    print("   DESCRIPTIVE ONLY — legacy rows with no paired blocks. Nothing here")
    print("   compares arms against each other: without pairing there is no honest")
    print("   way to separate an arm's contribution from the run's, so no frontier,")
    print("   dominance, or superiority claim is made or implied from these rows.")
    print("     %-10s %6s %6s  %11s %6s %6s %9s %9s %9s %11s %10s %10s"
          % ("arm", "tried", "succ", "success", "miss$$", "miss_s",
             "mean s", "p50 s", "p95 s", "total $", "mean $", "$/succ"))
    for arm in sorted(by_arm, key=str):
        print_stats_row(arm, arm_stats(by_arm[arm]))


def main():
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("results", nargs="+", help="one or more results.jsonl files")
    args = parser.parse_args()

    cells = load(args.results)
    if not cells:
        print("no cells to read")
        return 1

    excluded = []
    attempts = []
    for cell in cells:
        ok, why = attempt_status(cell)
        if not ok:
            excluded.append((cell.get("experiment_id"), stratum_of(cell), cell.get("arm"), why))
            continue
        if (cell.get("experiment_id") is None) != (cell.get("block_id") is None):
            # Half a pairing key cannot pair anything, and guessing the other
            # half would fabricate a block the rig never made.
            excluded.append((cell.get("experiment_id"), stratum_of(cell), cell.get("arm"),
                             "experiment_id and block_id must both be present to pair;"
                             " this row is neither paired nor descriptive"))
            continue
        attempts.append(cell)

    # Experimental cells route through paired blocks; the rest is description.
    experimental = collections.defaultdict(list)
    legacy = collections.defaultdict(lambda: collections.defaultdict(list))
    for cell in attempts:
        if cell.get("experiment_id") is not None:
            experimental[(cell["experiment_id"], stratum_of(cell))].append(cell)
        else:
            legacy[stratum_of(cell)][cell.get("arm")].append(cell)

    if experimental:
        print("== paired experiments (cells carrying experiment_id and block_id)")
        for (experiment_id, stratum) in sorted(experimental, key=lambda key: (str(key[0]), stratum_label(key[1]))):
            pair_experiment(experiment_id, stratum, experimental[(experiment_id, stratum)], excluded)

    if legacy:
        if experimental:
            print("\n== legacy rows (no pairing)")
        for stratum in sorted(legacy, key=stratum_label):
            print_legacy(stratum, legacy[stratum], excluded)

    if excluded:
        print("\n== excluded from every figure above")
        print("   an excluded row is not a pass and not a fail — it is a measurement")
        print("   that was not made, and it stays out of every denominator.")
        for experiment_id, stratum, arm, why in sorted(
                excluded, key=lambda e: (str(e[0] or ""), stratum_label(e[1]), str(e[2] or ""))):
            where = "experiment %s" % experiment_id if experiment_id else "legacy"
            print("   %-16s %-44s %-10s %s"
                  % (where, stratum_label(stratum), arm or "", why))
    return 0


if __name__ == "__main__":
    sys.exit(main())

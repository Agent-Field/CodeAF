#!/usr/bin/env python3
"""outcomes.py — read a directory of `go test -v` logs into one outcome per test.

PARITY IS AN OUTCOME CLAIM, and this is the file that turns seventeen minutes of
terminal screens into the thing the claim is made of: for every subtest of every
tagged suite, one of `pass`, `fail`, `skip` — and, when the log ends without the
test ever reporting, `incomplete`, which is a fourth word and not a fail.

The fourth word matters. A run killed by its own `-timeout`, or by the ssh pipe
going away, leaves subtests that started and never ended; calling those failures
would invent a regression, and calling them passes would hide one. They are
reported as what they are, and the comparison refuses to call a pair equal when
either side is incomplete.

Usage: outcomes.py <dir-of-logs>   → JSON on stdout
"""

import json
import os
import re
import sys

# `go test -v` indents a subtest's verdict under its parent, so the leading
# whitespace is part of the grammar rather than noise to be stripped.
VERDICT = re.compile(r"^\s*--- (PASS|FAIL|SKIP): (\S+) \(([0-9.]+)s\)")
RUN = re.compile(r"^=== RUN\s+(\S+)")
WORD = {"PASS": "pass", "FAIL": "fail", "SKIP": "skip"}


def read(path):
    """One log file's tests, in the order the runner reported them."""
    started, ended, seconds = [], {}, {}
    with open(path, "r", errors="replace") as handle:
        for line in handle:
            if match := RUN.match(line):
                if match.group(1) not in started:
                    started.append(match.group(1))
                continue
            if match := VERDICT.match(line):
                ended[match.group(2)] = WORD[match.group(1)]
                seconds[match.group(2)] = float(match.group(3))
    return started, ended, seconds


def main():
    root = sys.argv[1]
    suites = {}
    for name in sorted(os.listdir(root)):
        if not name.endswith(".log"):
            continue
        suite = name[: -len(".log")]
        started, ended, seconds = read(os.path.join(root, name))
        tests = {}
        for test in started:
            tests[test] = {
                "outcome": ended.get(test, "incomplete"),
                "seconds": seconds.get(test),
            }
        # THE PARENT IS NOT COUNTED BESIDE ITS OWN CHILDREN. `go test -v`
        # reports the top-level test as failing whenever any subtest did, so
        # tallying every reported name turns one broken subtest into two
        # failures and a suite of fifteen into a suite of sixteen. Leaves are
        # what a parity claim is made of; the parent's row is kept in `tests`
        # because a parent that failed with no failing child is a real and
        # otherwise invisible event (a fixture that died before any subtest).
        leaves = {name: entry for name, entry in tests.items() if "/" in name}
        counted = leaves or tests
        tally = {word: 0 for word in ("pass", "fail", "skip", "incomplete")}
        for entry in counted.values():
            tally[entry["outcome"]] += 1
        suites[suite] = {"tests": tests, "tally": tally}
    json.dump(suites, sys.stdout, indent=2, sort_keys=True)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()

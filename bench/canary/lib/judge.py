#!/usr/bin/env python3
"""Grade one cell's working tree with the fix pull request's own tests.

The harness never sees these tests. They are taken from the merge commit in the
mirror AFTER the door has stopped and laid over whatever the door left, so a
door that rewrote a test to make it pass is graded by the original. Two
readings come out: the pull request's test files alone (the fail-to-pass set,
the one that says the issue is fixed) and the whole suite (the one that says
nothing else broke). The whole-suite figure is recorded against the base
counts the pool measured, and is a regression only when a base count exists to
regress from.

The door's own diff is kept beside the grade, taken BEFORE the overlay, because
when a cell fails the diff is the only evidence of what the model actually did.
"""
import argparse
import json
import os
import re
import subprocess
import sys


def sh(args, cwd=None, cap=None):
    """Run one command and answer (exit code, combined output); a cap that hits reads as exit 124."""
    try:
        done = subprocess.run(args, cwd=cwd, capture_output=True, text=True, timeout=cap)
        return done.returncode, done.stdout + done.stderr
    except subprocess.TimeoutExpired as expired:
        return 124, (expired.stdout or "") + (expired.stderr or "")


def counts(output):
    """Pull pytest's summary counts out of its last line; absent words are zero."""
    tail = output.strip().splitlines()[-1] if output.strip() else ""
    found = {word: 0 for word in ("passed", "failed", "error", "errors")}
    for number, word in re.findall(r"(\d+) (passed|failed|errors?)", tail):
        found[word] = int(number)
    return {"passed": found["passed"], "failed": found["failed"], "errors": found["error"] + found["errors"]}


def keep_diff(work, out):
    """Record what the door changed: the tracked diff and the untracked names."""
    _, status = sh(["git", "status", "--porcelain"], cwd=work)
    _, diff = sh(["git", "diff"], cwd=work)
    with open(os.path.join(out, "work.patch"), "w") as handle:
        handle.write(diff)
    with open(os.path.join(out, "work.status"), "w") as handle:
        handle.write(status)
    return len([line for line in status.splitlines() if line.strip()])


def overlay(work, mirror, merge, tests):
    """Write the merge commit's version of every test file into the tree."""
    for path in tests:
        code, body = sh(["git", "show", f"{merge}:{path}"], cwd=mirror)
        if code != 0:
            raise SystemExit(f"cannot read {path} at {merge}: {body}")
        full = os.path.join(work, path)
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "w") as handle:
            handle.write(body)


def pytest(work, targets, cap, log):
    python = os.path.join(work, ".venv", "bin", "python")
    code, output = sh([python, "-m", "pytest", "-q", "-p", "no:cacheprovider", *targets], cwd=work, cap=cap)
    with open(log, "w") as handle:
        handle.write(output)
    return code, counts(output)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--work", required=True)
    parser.add_argument("--mirror", required=True)
    parser.add_argument("--merge", required=True)
    parser.add_argument("--tests", nargs="+", required=True)
    parser.add_argument("--base-suite", default="", help="JSON of the pool's base counts, or empty")
    parser.add_argument("--out", required=True, help="directory for judge.json and the logs")
    parser.add_argument("--suite-cap", type=int, default=600)
    args = parser.parse_args()

    changed = keep_diff(args.work, args.out)
    overlay(args.work, args.mirror, args.merge, args.tests)
    f2p_code, f2p = pytest(args.work, args.tests, 600, os.path.join(args.out, "f2p.log"))
    suite_code, suite = pytest(args.work, [], args.suite_cap, os.path.join(args.out, "suite.log"))

    base = json.loads(args.base_suite) if args.base_suite.strip() else None
    regressed = None
    if base and suite_code != 124:
        regressed = suite["failed"] + suite["errors"] > base.get("failed", 0) + base.get("errors", 0)
    json.dump({
        "changed_files": changed,
        "f2p": {**f2p, "exit": f2p_code, "pass": f2p_code == 0 and f2p["passed"] > 0},
        "suite": {**suite, "exit": suite_code, "capped": suite_code == 124},
        "regressed": regressed,
    }, open(os.path.join(args.out, "judge.json"), "w"), indent=1)


if __name__ == "__main__":
    main()

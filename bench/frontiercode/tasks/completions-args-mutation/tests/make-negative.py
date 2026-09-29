#!/usr/bin/env python3
"""Generate the mis-aimed-copy negative control for the completions task.

This is the labelled failure shape for this task's two blockers: the patch
adds a defensive copy of the WRONG slice (a fresh `args` copy that nothing
consumes), so the slice handed through completion lookup stays a subslice of
the caller's arguments (blocker 1: the regression test still sees os.Args
mutated) and `trimmedArgs` is still not a fresh copy the machinery owns
(blocker 2: "is the slice handed through completion lookup a FRESH COPY").
Both blocker questions are answered from the patch alone; every step of the
generation asserts, so a drifted upstream tree fails this script at image
build instead of quietly producing a mislabelled control.

The script runs INSIDE the verifier image, in a checkout of the base commit.
All edits are described as transformations of the base tree's own text — no
upstream bytes are committed to the rig.
"""

import argparse
import pathlib
import subprocess
import sys

TRIM_ANCHOR = "\ttrimmedArgs := args[:len(args)-1]"
DEAD_COPY = (
    "\n"
    "\t// Defensive copy: keep our own copy of the arguments so nothing we\n"
    "\t// do later can disturb the caller's slice.\n"
    "\townedArgs := make([]string, len(args))\n"
    "\tcopy(ownedArgs, args)\n"
    "\t_ = ownedArgs\n"
)
APPEND_ANCHOR = '_ = finalCmd.ParseFlags(append(finalArgs, "--"))'


def git(repo: pathlib.Path, *args: str) -> str:
    out = subprocess.run(
        ["git", "-C", str(repo), *args], check=True, capture_output=True, text=True
    )
    return out.stdout


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", required=True)
    ap.add_argument("--out", required=True)
    args = ap.parse_args()

    repo = pathlib.Path("/solution/full")
    completions_go = repo / "completions.go"
    base = completions_go.read_text()

    # Anchor 1: the aliasing slice exists at base and is NOT already copied.
    assert base.count(TRIM_ANCHOR) == 1, "trimmed-args anchor drifted"
    assert "copy(trimmedArgs" not in base, "base already copies the trimmed args"

    # Anchor 2: the mutating append exists at base (the site the negative
    # control must leave untouched so the regression test keeps failing).
    assert base.count(APPEND_ANCHOR) == 1, "append-site anchor drifted"

    patched = base.replace(TRIM_ANCHOR, TRIM_ANCHOR + DEAD_COPY, 1)
    assert patched != base, "no edit applied"
    # Both blockers, by construction: trimmedArgs is still the aliased
    # subslice, and the mutating append site is untouched.
    assert "copy(trimmedArgs" not in patched
    assert "copy(ownedArgs, args)" in patched
    assert APPEND_ANCHOR in patched
    completions_go.write_text(patched)

    git(repo, "add", "-A")
    diff = git(repo, "diff", "--binary", args.base)
    assert diff.strip(), "empty negative diff"
    assert "copy(ownedArgs, args)" in diff
    pathlib.Path(args.out).write_text(diff)
    print(f"negative control written: {args.out} ({len(diff)} bytes)")
    return 0


if __name__ == "__main__":
    sys.exit(main())

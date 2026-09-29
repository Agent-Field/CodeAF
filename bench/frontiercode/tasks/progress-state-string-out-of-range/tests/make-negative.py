#!/usr/bin/env python3
"""Generate the respelled-known-name negative control for the
progress-state task.

This is the labelled failure shape for this task's two blockers: String() is
made total, but the out-of-range placeholder is NOT the regression test's
expected value (blocker 1: the classical test
TestProgressBarStateStringOutOfRange fails — the test requires "Unknown" for
-1, 5 and 100), and the same patch respells a KNOWN state's name (blocker 2:
"a rewrite that also respells a known name ... fails"). Both blocker
questions are answered from the patch alone; every step of the generation
asserts, so a drifted upstream tree fails this script at image build instead
of quietly producing a mislabelled control.

The script runs INSIDE the verifier image, in a checkout of the base commit.
All edits are described as transformations of the base tree's own text — no
upstream bytes are committed to the rig.
"""

import argparse
import pathlib
import subprocess
import sys

ANCHOR = '''func (s ProgressBarState) String() string {
\treturn [...]string{
\t\t"None",
\t\t"Default",
\t\t"Error",
\t\t"Indeterminate",
\t\t"Warning",
\t}[s]
}'''

REPLACEMENT = '''func (s ProgressBarState) String() string {
\tnames := [...]string{
\t\t"None",
\t\t"Unexpected", // known state respelled: blocker 2
\t\t"Error",
\t\t"Indeterminate",
\t\t"Warning",
\t}
\tif int(s) < 0 || int(s) >= len(names) {
\t\treturn "out of range" // not the placeholder the fix returns: blocker 1
\t}
\treturn names[s]
}'''


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
    tea_go = repo / "tea.go"
    base = tea_go.read_text()

    # Anchor 1: the indexing String() exists at base, exactly once.
    assert base.count(ANCHOR) == 1, "string-method anchor drifted"

    # Anchor 2: the base panics on out-of-range (no guard, no switch/default).
    assert "default:" not in base.split("func (s ProgressBarState) String() string {")[1].split("\n}\n")[0], \
        "base already total"
    assert base.count(ANCHOR) == 1 and "Unknown" not in base, "base already returns a placeholder"

    patched = base.replace(ANCHOR, REPLACEMENT, 1)
    assert patched != base, "no edit applied"
    assert patched.count("int(s) < 0") == 1
    assert patched.count('"Unexpected"') == 1
    tea_go.write_text(patched)

    git(repo, "add", "-A")
    diff = git(repo, "diff", "--binary", args.base)
    assert diff.strip(), "empty negative diff"
    # Both blockers, by construction: a known name is respelled, and the
    # out-of-range value does not return the placeholder the fix returns.
    assert '"Unexpected"' in patched
    assert '"out of range"' in patched
    pathlib.Path(args.out).write_text(diff)
    print(f"negative control written: {args.out} ({len(diff)} bytes)")
    return 0


if __name__ == "__main__":
    sys.exit(main())

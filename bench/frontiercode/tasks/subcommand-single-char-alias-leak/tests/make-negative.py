#!/usr/bin/env python3
"""Generate the labelled negative control for the single-char-alias task.

This is the labelled failure shape for this task's two blockers: the change
INVERTS the form check in the "unsee" loop — a one-character alias is still
unseen under its long form, and a longer alias is unseen under its short
form. The single-character regression test therefore still errors (blocker 1
fails: the alias is not deleted under the form it is invoked as), and the
conflict contract is broken for every other flag — long aliases now lose
their duplicate detection (blocker 2 fails: the change treats long aliases as
short). Both blocker questions are answered from the patch alone; every step
of the generation asserts, so a drifted upstream tree fails this script at
image build instead of quietly producing a mislabelled control.

The script runs INSIDE the verifier image, in a checkout of the base commit.
All edits are described as transformations of the base tree's own text — no
upstream bytes are committed to the rig.
"""

import argparse
import pathlib
import subprocess
import sys

# The "unsee" loop at base: every alias is deleted under its long form only —
# the bug the real fix must address.
UNSEE_ANCHOR = '''\t\tfor _, aflag := range flag.Aliases {
\t\t\tdelete(seenFlags, "--"+aflag)
\t\t}
'''
# The same loop with the form check inverted: single-character aliases are
# still unseen as long, long aliases as short. The bug survives for the very
# case the brief names, and the contract breaks for every other alias.
UNSEE_INVERTED = '''\t\tfor _, aflag := range flag.Aliases {
\t\t\tif utf8.RuneCountInString(aflag) == 1 {
\t\t\t\tdelete(seenFlags, "--"+aflag)
\t\t\t} else {
\t\t\t\tdelete(seenFlags, "-"+aflag)
\t\t\t}
\t\t}
'''


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
    build_go = repo / "build.go"
    base = build_go.read_text()

    # Anchor: the "unsee" loop exists at base, exactly once, still long-form
    # only.
    assert base.count(UNSEE_ANCHOR) == 1, "unsee-loop anchor drifted"
    assert "delete(seenFlags, \"-\")" not in base

    patched = base.replace(UNSEE_ANCHOR, UNSEE_INVERTED, 1)
    assert patched != base, "no edit applied"
    assert patched.count(UNSEE_INVERTED) == 1
    build_go.write_text(patched)

    git(repo, "add", "-A")
    diff = git(repo, "diff", "--binary", args.base)
    assert diff.strip(), "empty negative diff"
    # Both blockers, by construction: single-character aliases are still
    # deleted under the wrong form, and long aliases lose their form.
    assert 'delete(seenFlags, "--"+aflag)' in patched
    assert 'delete(seenFlags, "-"+aflag)' in patched
    pathlib.Path(args.out).write_text(diff)
    print(f"negative control written: {args.out} ({len(diff)} bytes)")
    return 0


if __name__ == "__main__":
    sys.exit(main())

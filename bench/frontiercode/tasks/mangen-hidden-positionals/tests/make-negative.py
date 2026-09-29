#!/usr/bin/env python3
"""Generate the drop-all-positionals negative control for the mangen task.

This is the labelled failure shape for this task's two blockers: the change
does NOT filter hidden positionals by their hide flag (blocker 1: the
hidden-positional snapshot test keeps failing), and it drops EVERY positional
from the SYNOPSIS, visible ones included (blocker 2: "a change that drops
visible positionals too ... fails"). Both blocker questions are answered from
the patch alone; every step of the generation asserts, so a drifted upstream
tree fails this script at image build instead of quietly producing a
mislabelled control.

The script runs INSIDE the verifier image, in a checkout of the base commit.
All edits are described as transformations of the base tree's own text — no
upstream bytes are committed to the rig.
"""

import argparse
import pathlib
import subprocess
import sys

LOOP_ANCHOR = "for arg in cmd.get_positionals() {"
DROP_ALL = "for arg in cmd.get_positionals().filter(|_| false) {"
HIDE_FILTER = ".filter(|arg| !arg.is_hide_set())"


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
    render_rs = repo / "clap_mangen" / "src" / "render.rs"
    base = render_rs.read_text()

    # Anchor 1: the positional loop exists at base and carries NO hide filter.
    assert base.count(LOOP_ANCHOR) == 1, "positional-loop anchor drifted"
    assert HIDE_FILTER not in base, "base already filters hidden positionals"

    patched = base.replace(LOOP_ANCHOR, DROP_ALL, 1)
    assert patched != base, "no edit applied"
    assert patched.count(DROP_ALL) == 1
    # Both blockers, by construction: the hide flag is still ignored, and
    # every positional — visible ones included — is dropped from the SYNOPSIS.
    assert HIDE_FILTER not in patched
    assert ".filter(|_| false)" in patched
    render_rs.write_text(patched)

    git(repo, "add", "-A")
    diff = git(repo, "diff", "--binary", args.base)
    assert diff.strip(), "empty negative diff"
    pathlib.Path(args.out).write_text(diff)
    print(f"negative control written: {args.out} ({len(diff)} bytes)")
    return 0


if __name__ == "__main__":
    sys.exit(main())

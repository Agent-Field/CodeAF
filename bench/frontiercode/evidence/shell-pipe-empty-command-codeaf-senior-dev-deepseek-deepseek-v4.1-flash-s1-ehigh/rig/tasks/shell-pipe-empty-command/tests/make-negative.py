#!/usr/bin/env python3
"""Generate the error-on-empty negative control for the shell-pipe task.

This is the labelled failure shape for this task's two blockers: the empty
case is guarded, but the guard converts the skip into an ERROR (blocker 1:
"a change that converts the skip into an error fails"), and the same patch
swallows failures of NON-empty commands (blocker 2: "a change that swallows
failures fails"). Both blocker questions are answered from the patch alone;
every step of the generation asserts, so a drifted upstream tree fails this
script at image build instead of quietly producing a mislabelled control.

The script runs INSIDE the verifier image, in a checkout of the base commit.
All edits are described as transformations of the base tree's own text — no
upstream bytes are committed to the rig.
"""

import argparse
import pathlib
import subprocess
import sys

GUARD_ANCHOR = "func Run(ctx *context.Context, dir string, command, env []string, output bool) error {"
GUARD = '''\tif len(command) == 0 {
\t\tlog.Warn("empty command")
\t\treturn fmt.Errorf("shell: empty command")
\t}
'''
RUN_ANCHOR = "\tif err := cmd.Run(); err != nil {\n\t\treturn fmt.Errorf("
RUN_SWALLOW = "\tif err := cmd.Run(); err != nil {\n\t\tlog.WithError(err).Warn(\"command failed\")\n\t}\n\tif false {\n\t\treturn fmt.Errorf("


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
    shell_go = repo / "internal" / "shell" / "shell.go"
    base = shell_go.read_text()

    # Anchor 1: the run function exists at base and has NO empty-case guard.
    assert base.count(GUARD_ANCHOR) == 1, "run-function anchor drifted"
    assert "len(command) == 0" not in base, "base already guards the empty case"

    # Anchor 2: the failure path exists at base and returns an error.
    assert base.count(RUN_ANCHOR) == 1, "run error-path anchor drifted"

    patched = base.replace(GUARD_ANCHOR, GUARD_ANCHOR + "\n" + GUARD.rstrip("\n") + "\n", 1)
    patched = patched.replace(
        "\tif err := cmd.Run(); err != nil {\n\t\treturn fmt.Errorf(",
        RUN_SWALLOW,
        1,
    )
    assert patched != base, "no edit applied"
    assert patched.count("len(command) == 0") == 1
    shell_go.write_text(patched)

    git(repo, "add", "-A")
    diff = git(repo, "diff", "--binary", args.base)
    assert diff.strip(), "empty negative diff"
    # Both blockers, by construction: the guard returns an error, and the
    # error path of non-empty commands is swallowed.
    assert 'return fmt.Errorf("shell: empty command")' in patched
    assert 'log.WithError(err).Warn("command failed")' in patched
    pathlib.Path(args.out).write_text(diff)
    print(f"negative control written: {args.out} ({len(diff)} bytes)")
    return 0


if __name__ == "__main__":
    sys.exit(main())

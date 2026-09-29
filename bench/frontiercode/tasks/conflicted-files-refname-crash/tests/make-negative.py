#!/usr/bin/env python3
"""Generate the catch-and-partial-set negative control for this task.

This is the labelled failure shape for the two blockers: the ambiguity is
"handled" by catching the error and returning a PARTIAL set (only the
merge-conflict filenames), which is silent data loss — exactly what the
completeness blocker forbids. Both blockers fail; the test-side regression
test also fails because the tracked file named HEAD never appears in the
returned set.

The script runs INSIDE the verifier image, in a checkout of the base commit.
All edits are transformations of the base tree's own text; every step asserts.
"""

import argparse
import pathlib
import subprocess
import sys

ANCHOR = """    merge_diff_filenames = zsplit(
        cmd_output(
            'git', 'diff', '--name-only', '--no-ext-diff', '-z',
            '-m', tree_hash, 'HEAD', 'MERGE_HEAD',
        )[1],
    )
"""
NEGATIVE = """    try:
        merge_diff_filenames = zsplit(
            cmd_output(
                'git', 'diff', '--name-only', '--no-ext-diff', '-z',
                '-m', tree_hash, 'HEAD', 'MERGE_HEAD',
            )[1],
        )
    except Exception:
        # the labelled negative: the ambiguity is swallowed and the
        # merge-diff half of the set is silently dropped
        merge_diff_filenames = set()
"""


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
    git_py = repo / "pre_commit" / "git.py"
    base = git_py.read_text()

    assert base.count(ANCHOR) == 1, "get_conflicted_files anchor drifted"
    assert "except Exception" not in base, "base already swallows the ambiguity"

    patched = base.replace(ANCHOR, NEGATIVE, 1)
    assert patched != base and patched.count("except Exception") == 1
    git_py.write_text(patched)

    git(repo, "add", "-A")
    diff = git(repo, "diff", "--binary", args.base)
    assert diff.strip(), "empty negative diff"
    pathlib.Path(args.out).write_text(diff)
    print(f"negative control written: {args.out} ({len(diff)} bytes)")
    return 0


if __name__ == "__main__":
    sys.exit(main())

#!/usr/bin/env python3
"""Generate the first-line-only negative control for the jsonschema task.

This is the labelled example's failure shape: the helper is added with the
right behaviour, every single-line call site is converted, the reference test
is updated — but each of the two multi-line warnings is converted only down to
its first line, leaving the continuation statements writing bare stderr
without the `warning: ` prefix. A maintainer would not merge that; both
blockers must fail while every non-blocker passes.

The script runs INSIDE the verifier image, in a checkout of the base commit
with the reference patch applied. The leftover statements it keeps are read
from the base commit's own text of the two files, so the rig commits no
upstream source bytes: only this transformation logic is ours. Every step
asserts; a drifted or mis-generated control fails this script instead of
quietly grading 1.00.
"""

import argparse
import pathlib
import re
import subprocess
import sys

# Per blocker file: the marker that finds the multi-line warning's first
# statement in the base tree, how many bare stderr statements the base block
# carries after it (the ones a first-line-only conversion leaves behind), and
# the first message's line count in the reference patch's own shape — the
# lines the control keeps, closed with the semicolon the chain's continuation
# denied them.
BLOCKERS = {
    "src/resolver.h": {"prefix_marker": "warning: No schema resources", "leftover_stderr": 2, "first_lines": 1},
    "src/command_bundle.cc": {"prefix_marker": "warning: You are opting in", "leftover_stderr": 5, "first_lines": 3},
}


def git(repo: pathlib.Path, *args: str) -> str:
    out = subprocess.run(
        ["git", "-C", str(repo), *args], check=True, capture_output=True, text=True
    )
    return out.stdout


def literals(text: str) -> str:
    """Concatenate the double-quoted literals of one statement — the message
    the statement writes."""
    return "".join(re.findall(r'"((?:[^"\\]|\\.)*)"', text))


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--base", required=True, help="base commit sha")
    parser.add_argument("--out", required=True, help="where the patch is written")
    args = parser.parse_args()
    repo = pathlib.Path("/solution/full")

    # The tree starts at the base commit and takes the reference patch whole:
    # the helper, the include, the single-line conversions and the reference
    # test update all stand. The two blocker files are then rewritten below.
    git(repo, "checkout", "--quiet", "--detach", args.base)
    git(repo, "apply", "--whitespace=nowarn", "/solution/upstream-reference.patch")

    for path, spec in BLOCKERS.items():
        file = repo / path
        ref = file.read_text().split("\n")
        base = git(repo, "show", f"{args.base}:{path}").split("\n")

        # The reference tree's converted block: from its first LOG_WARNING
        # line to the statement's closing semicolon.
        ref_start = next(i for i, line in enumerate(ref) if "LOG_WARNING()" in line)
        ref_end = ref_start
        while not ref[ref_end].rstrip().endswith(";"):
            ref_end += 1

        # The first message's own lines: walk the chain until a literal closes
        # the message line with a newline escape, then give that line the
        # statement semicolon the chain took away. What the chain wrote after
        # the first message is dropped — that is the point of the control.
        kept = list(ref[ref_start : ref_start + spec["first_lines"]])
        if not literals("\n".join(kept)).endswith("\\n"):
            sys.exit(f"{path}: the kept lines do not close the first message")
        kept[-1] = kept[-1].rstrip().removesuffix(";") + ";"
        if "warning: " in "\n".join(kept):
            sys.exit(f"{path}: the kept first message still spells the prefix")

        # The base block: its first statement (carrying the prefix) and the
        # bare stderr statements after it, which the control leaves behind.
        base_start = next(i for i, line in enumerate(base) if spec["prefix_marker"] in line)
        while "std::cerr" not in base[base_start]:
            base_start -= 1
        base_end = base_start
        while not base[base_end].rstrip().endswith(";"):
            base_end += 1
        if "warning: " not in literals("\n".join(base[base_start : base_end + 1])):
            sys.exit(f"{path}: the base statement does not carry the warning prefix")
        leftovers = [line for line in base[base_end + 1 :] if "std::cerr" in line][
            : spec["leftover_stderr"]
        ]
        if len(leftovers) != spec["leftover_stderr"]:
            sys.exit(
                f"{path}: expected {spec['leftover_stderr']} leftover stderr "
                f"statements in the base block, found {len(leftovers)}"
            )
        last_leftover = leftovers[-1]
        keep_from = base.index(last_leftover) + 1
        leftover_lines = base[base_end + 1 : keep_from]

        # Splice: the reference tree up to the block, the first message on its
        # own, the base block's remaining statements verbatim, then the rest
        # of the reference tree.
        negative = list(ref[:ref_start]) + kept + leftover_lines + list(ref[ref_end + 1 :])
        file.write_text("\n".join(negative))

    bundle = (repo / "src/command_bundle.cc").read_text()
    if '#include "logger.h"' not in bundle:
        sys.exit("src/command_bundle.cc: the control lacks the logger include")

    patch = git(repo, "diff", args.base, "HEAD", "--binary")
    if not patch.strip():
        sys.exit("the negative control is an empty diff")
    pathlib.Path(args.out).write_text(patch)
    print(f"negative control: {len(patch)} bytes -> {args.out}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
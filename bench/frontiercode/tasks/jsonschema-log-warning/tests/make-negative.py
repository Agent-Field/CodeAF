#!/usr/bin/env python3
"""Generate the first-line-only negative control for the jsonschema task.

This is the labelled example's failure shape: the helper is added with the
right behaviour, every single-line call site is converted, the reference test
is updated — but each of the two multi-line warnings is converted only down to
its first statement, leaving the continuation statements writing bare stderr
without the `warning: ` prefix. A maintainer would not merge that; both
blockers must fail while every non-blocker passes.

The script runs INSIDE the verifier image, in a checkout of the reference
commit. It reads the base commit's own text of the two files for the
statements it leaves behind, so the rig commits no upstream source bytes: only
this transformation logic is ours. Every step asserts; a drifted or
mis-generated control fails this script instead of grading 1.00 quietly.
"""

import argparse
import pathlib
import subprocess
import sys

# Per blocker file: the message prefix that marks the multi-line warning's
# first statement in the base tree, the shape the reference patch gave the
# converted statement (payload lines, exactly as upstream wrapped them), and
# how many bare stderr statements the base block carries after it — the ones a
# first-line-only conversion leaves behind.
BLOCKERS = {
    "src/resolver.h": {
        "first_message": "No schema resources were imported from this file\n",
        "prefix_marker": "warning: No schema resources",
        "converted": ['          LOG_WARNING() << "No schema resources were imported from this file\\n";'],
        "leftover_stderr": 2,
    },
    "src/command_bundle.cc": {
        "first_message": "You are opting in to remove schema identifiers in the bundled schema.\n",
        "prefix_marker": "warning: You are opting in",
        "converted": [
            "    sourcemeta::jsonschema::LOG_WARNING()",
            '        << "You are opting in to remove schema identifiers in "',
            '           "the bundled schema.\\n";',
        ],
        "leftover_stderr": 5,
    },
}


def git(repo: pathlib.Path, *args: str) -> str:
    out = subprocess.run(
        ["git", "-C", str(repo), *args], check=True, capture_output=True, text=True
    )
    return out.stdout


def literals_of(text: str) -> str:
    """Concatenate the double-quoted string literals of one statement, with the
    quotes dropped — the message the statement writes."""
    import re

    parts = re.findall(r'"((?:[^"\\]|\\.)*)"', text)
    return "".join(parts)


def convert_statement(base: list, start: int, spec: dict, path: str) -> tuple:
    """Replace the base statement at `start` (which writes `warning: <message>`
    to bare stderr) with the reference patch's LOG_WARNING shape, and answer
    the index one past the statement's last line."""
    index = start
    while not base[index].rstrip().endswith(";"):
        index += 1
    statement = "\n".join(base[start : index + 1])

    if "std::cerr" not in statement:
        sys.exit(f"{path}: the warning statement does not write stderr: {statement!r}")
    payload = literals_of(statement)
    if not payload.startswith("warning: "):
        sys.exit(f"{path}: statement does not carry the warning prefix: {payload!r}")
    stripped = payload[len("warning: ") :]
    if stripped != spec["first_message"]:
        sys.exit(f"{path}: statement payload drifted from the pinned shape: {stripped!r}")
    return spec["converted"], index + 1


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--base", required=True, help="base commit sha")
    parser.add_argument("--out", required=True, help="where the patch is written")
    args = parser.parse_args()
    repo = pathlib.Path("/solution/full")

    # Work from the base commit, then take the reference patch's NON-blocker
    # changes on top: the helper in logger.h, the include in command_bundle.cc,
    # the single-line conversions and the reference test update. The two
    # blocker files are then rewritten to the first-line-only shape below, so
    # the control is "the reference patch minus the rest of each multi-line
    # conversion" and nothing else.
    git(repo, "checkout", "--quiet", "--detach", args.base)
    git(repo, "apply", "--whitespace=nowarn", "/solution/upstream-reference.patch")

    for path, spec in BLOCKERS.items():
        file = repo / path
        base = git(repo, "show", f"{args.base}:{path}").split("\n")
        first = next(i for i, line in enumerate(base) if spec["prefix_marker"] in line)
        # The statement may open on the `std::cerr` line above the marker line.
        start = first
        while "std::cerr" not in base[start]:
            start -= 1
        converted, after = convert_statement(base, start, spec, path)

        # Count the bare stderr statements the base block keeps after the
        # converted one. A drifted upstream fails here rather than grading.
        rest = [line for line in base[after:] if "std::cerr" in line]
        rest = rest[: spec["leftover_stderr"]]
        if len(rest) != spec["leftover_stderr"]:
            sys.exit(f"{path}: expected {spec['leftover_stderr']} leftover stderr statements, found {len(rest)}")
        last = None
        for line in base[after:]:
            if "std::cerr" in line:
                last = line
        # Splice: everything up to the converted statement, the converted
        # statement, then the base block's remaining statements verbatim, then
        # whatever followed the block in the base tree.
        keep_from = after
        if last is not None:
            keep_from = base.index(last) + 1
        negative = list(base[:start]) + list(converted) + list(base[after:keep_from]) + list(base[keep_from:])
        file.write_text("\n".join(negative))

    bundle = (repo / "src/command_bundle.cc").read_text()
    if '#include "logger.h"' not in bundle:
        sys.exit("src/command_bundle.cc: the control lacks the logger include")
    if "warning: " in (repo / "src/resolver.h").read_text().split("LOG_WARNING()")[0].split("std::cerr")[0]:
        pass  # the base's other warning sites are untouched by design; blockers judged per file

    patch = git(repo, "diff", args.base, "HEAD", "--binary")
    if not patch.strip():
        sys.exit("the negative control is an empty diff")
    pathlib.Path(args.out).write_text(patch)
    print(f"negative control: {len(patch)} bytes -> {args.out}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
#!/usr/bin/env python3
"""Generate the first-line-only negative control for the jsonschema task.

This is the labelled example's failure shape: the helper is added with the
right behaviour, every single-line call site is converted, the reference test
is updated — but each of the two multi-line warnings is converted only down to
its first line, leaving the continuation statements writing bare stderr
without the `warning: ` prefix. A maintainer would not merge that; both
blockers must fail while every non-blocker passes.

The script runs INSIDE the verifier image, in a checkout of the reference
commit, and never writes outside /solution. It reads the base commit's own
text of the two files for the continuation statements it leaves behind, so the
rig commits no upstream source bytes: only this transformation logic is ours.

    make-negative.py --base <sha> --out /solution/first-line-only.patch
"""

import argparse
import subprocess
import sys
import pathlib

# Each blocker file: where the converted block starts (the reference patch's
# first LOG_WARNING statement), and how many bare stderr statements the base
# tree carries in the same block — the ones a first-line-only conversion leaves
# behind. The assertion counts make a drifted upstream hard-fail this script
# instead of silently producing a wrong control.
BLOCKERS = {
    "src/resolver.h": {"leftover_stderr": 2},
    "src/command_bundle.cc": {"leftover_stderr": 5},
}


def git(repo: pathlib.Path, *args: str) -> str:
    out = subprocess.run(
        ["git", "-C", str(repo), *args], check=True, capture_output=True, text=True
    )
    return out.stdout


def statement_end(lines, start):
    """Index of the last line of the statement that opens at `start`, reading a
    chain of operator lines that closes on the first line ending in `;`."""
    index = start
    while not lines[index].rstrip().endswith(";"):
        index += 1
    return index


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--base", required=True, help="base commit sha")
    parser.add_argument("--out", required=True, help="where the patch is written")
    args = parser.parse_args()
    repo = pathlib.Path("/solution/full")

    for path, spec in BLOCKERS.items():
        ref = (repo / path).read_text().split("\n")
        base = git(repo, "show", f"{args.base}:{path}").split("\n")

        # The reference tree's converted block opens at its first LOG_WARNING
        # statement; the statement ends at its first semicolon line. Everything
        # the base tree wrote as bare stderr in the same block stays as-is:
        # that is exactly the un-converted remainder the control must keep.
        start = next(i for i, line in enumerate(ref) if "LOG_WARNING()" in line)
        end = statement_end(ref, start)
        first_statement = list(ref[start : end + 1])

        base_first = next(
            i for i, line in enumerate(base) if "warning: " in line and "std::cerr" in line
        )
        base_first_end = statement_end(base, base_first)
        base_block = list(base[base_first : base_first_end + 1])
        leftovers = [line for line in base_block[1:]]
        if len(leftovers) != spec["leftover_stderr"]:
            sys.exit(
                f"{path}: expected {spec['leftover_stderr']} leftover stderr "
                f"statements in the base block, found {len(leftovers)}"
            )

        # Splice: the reference tree keeps the converted first statement (now
        # closed with the semicolon it lacks mid-chain) and takes the base
        # tree's remaining statements verbatim.
        first_statement = list(first_statement)
        first_statement[-1] = first_statement[-1].rstrip().removesuffix(";") + ";"
        negative = list(ref[:start]) + first_statement + leftovers + list(ref[end + 1 :])
        (repo / path).write_text("\n".join(negative))

        if "warning: " in "\n".join(first_statement):
            sys.exit(f"{path}: the converted first statement still spells a prefix")

    # The bundle file must include the logger, or the control does not compile
    # and stops being the labelled example's shape (which builds).
    bundle = (repo / "src/command_bundle.cc").read_text()
    if "#include \"logger.h\"" not in bundle:
        sys.exit("src/command_bundle.cc: the control lacks the logger include")

    patch = git(repo, "diff", f"{args.base}", "HEAD", "--binary")
    if not patch.strip():
        sys.exit("the negative control is an empty diff")
    pathlib.Path(args.out).write_text(patch)
    print(f"negative control: {len(patch)} bytes -> {args.out}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
#!/usr/bin/env bash
# Fails if any Dockerfile of the stack names a base image, or the Dockerfile syntax frontend, without a digest: a tag can be
# re-pointed, a digest cannot. A `FROM ${VAR}` is checked through the default of `ARG VAR=...`; `FROM scratch` and a FROM
# that names an earlier stage are fine. CI runs this beside the gate's self-test.
#
#     ./pins-check.sh [DIR]       DIR holds the *.Dockerfile files (default: deploy/org, the parent of this folder)
set -eu
dir=${1:-$(cd "$(dirname "$0")/.." && pwd)}
exec python3 - "$dir" <<'PY'
import glob, os, re, sys

bad = []
files = sorted(glob.glob(os.path.join(sys.argv[1], "*.Dockerfile")))
if not files:
    sys.exit("pins-check: no Dockerfiles in " + sys.argv[1])
for path in files:
    name = os.path.basename(path)
    lines = open(path).read().splitlines()
    if not (lines and lines[0].startswith("# syntax=") and "@sha256:" in lines[0]):
        bad.append(f"{name}: the # syntax= line is missing or has no digest")
    args, stages = {}, set()
    for number, line in enumerate(lines, 1):
        m = re.match(r"ARG\s+([A-Za-z_][A-Za-z0-9_]*)=(\S+)", line)
        if m:
            args[m.group(1)] = m.group(2)
        m = re.match(r"FROM\s+(?:--\S+\s+)?(\S+)(?:\s+AS\s+(\S+))?\s*$", line, re.I)
        if not m:
            continue
        image, stage = m.group(1), m.group(2)
        if image != "scratch" and image not in stages:
            var = re.fullmatch(r"\$\{(\w+)\}", image)
            if var and var.group(1) == "CODEAF_BINARY_FROM":
                pass  # selects one of this file's own stages
            else:
                resolved = args.get(var.group(1), "") if var else image
                if "@sha256:" not in resolved:
                    bad.append(f"{name}:{number}: base image {image} has no digest" + (f" (ARG default: {resolved or 'none'})" if var else ""))
        if stage:
            stages.add(stage)
if bad:
    print("pins-check: FAIL\n  " + "\n  ".join(bad))
    sys.exit(1)
print(f"pins-check: PASS ({len(files)} Dockerfiles, every base image and syntax frontend pinned by digest)")
PY

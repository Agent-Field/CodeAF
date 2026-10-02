#!/usr/bin/env python3
"""Partition the shared selector's concrete package list into runner legs."""
import json
from pathlib import Path
import subprocess
import sys

named = {"./internal/tui3": "tui3", "./internal/session": "session", "./cmd/codeaf": "codeaf"}
groups = {}
for package in sys.stdin.read().split():
    groups.setdefault(named.get(package, "rest"), []).append(package)
matrix = {"include": [{"leg": leg, "packages": " ".join(packages)}
                      for leg, packages in sorted(groups.items())]}
if sys.argv[1:2] == ["run-local"]:
    # A laptop has one box, so legs run sequentially there. Partitioning still
    # matters: the failure cap is per leg, exactly as on separate CI runners.
    status = 0
    for leg, packages in sorted(groups.items()):
        print(f"touched {leg}: {' '.join(packages)}", flush=True)
        runner = Path(__file__).resolve().with_name("touched-verdict.sh")
        result = subprocess.run([str(runner), "run", *sys.argv[2:], *packages])
        status = max(status, int(result.returncode != 0))
    sys.exit(status)
print("matrix=" + json.dumps(matrix, separators=(",", ":")))
print("has-tests=" + ("true" if groups else "false"))

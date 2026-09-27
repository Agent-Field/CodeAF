#!/usr/bin/env python3
"""Render unmodified tmux framebuffer captures using agg (no provider calls)."""
import argparse
import json
from pathlib import Path
import shutil
import subprocess

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("source", type=Path)
parser.add_argument("output", type=Path)
args = parser.parse_args()
args.output.mkdir(parents=True, exist_ok=True)
agg = shutil.which("agg")
if not agg:
    raise SystemExit("agg is required on PATH")

for source in sorted(args.source.glob("*.cast")):
    target = args.output / source.name
    shutil.copyfile(source, target)
    subprocess.run([agg, "--font-size", "16", "--theme", "dracula", str(target), str(target.with_suffix(".gif"))], check=True)
for pattern in ("*.txt", "*-models.json"):
    for source in sorted(args.source.glob(pattern)):
        shutil.copyfile(source, args.output / source.name)

# A labelled slideshow of actual screen captures, not a continuous recording.
frames = ["02-notification", "03-dismissed", "04-restored"]
sequence = []
for number, stem in enumerate(frames):
    rows = [json.loads(line) for line in (args.source / (stem + ".cast")).read_text().splitlines()]
    if number == 0:
        header = rows[0]
        header["title"] = "Task notification, dismissed, restored — screenshot sequence"
        sequence.append(header)
    sequence.append([number * 2, "o", rows[1][2]])
sequence.append([6, "o", ""])
cast = args.output / "notification-sequence.cast"
cast.write_text("\n".join(json.dumps(row) for row in sequence) + "\n")
subprocess.run([agg, "--font-size", "16", "--theme", "dracula", str(cast), str(cast.with_suffix(".gif"))], check=True)

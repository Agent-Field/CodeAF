#!/usr/bin/env python3
"""Check durable hosted outcomes and keep private diagnostics before cleanup."""

import argparse
import json
import os
from pathlib import Path
import re
import sqlite3
import sys


def secrets(copied_config=None):
    known = {value for name, value in os.environ.items()
             if name.endswith("_API_KEY") and len(value) > 8}

    def walk(value):
        if isinstance(value, dict):
            for name, child in value.items():
                if isinstance(child, str) and "key" in name.lower() and len(child) > 8:
                    known.add(child)
                else:
                    walk(child)
        elif isinstance(value, list):
            for child in value:
                walk(child)

    # A bound profile is deliberately never opened by the drive. Only the
    # legacy mode's own temporary credential copy can be supplied here.
    if copied_config and Path(copied_config).is_file():
        walk(json.loads(Path(copied_config).read_text()))
    return known


def redact(text, known):
    for value in known:
        text = text.replace(value, "[key struck out]")
    text = re.sub(r"(?i)(Bearer\s+)[^\s\"\\]+", r"\1[key struck out]", text)
    return re.sub(r"sk-or-[A-Za-z0-9_-]+", "[key struck out]", text)


def owned_files(home):
    # A worker-created link must never turn an evidence walk into a read of
    # another profile. Config files themselves are never evidence.
    for directory, dirs, files in os.walk(home, followlinks=False):
        dirs[:] = [name for name in dirs if not (Path(directory) / name).is_symlink()]
        for name in files:
            path = Path(directory) / name
            if name != "config.json" and not path.is_symlink():
                yield path


def outcomes(home):
    latest = {}
    for path in owned_files(home):
        if path.name != "tasks.jsonl":
            continue
        body = path.read_text()
        lines = body.splitlines()
        for index, line in enumerate(lines):
            try:
                row = json.loads(line)
            except json.JSONDecodeError:
                # A live append may be between writes. Do not accept an
                # earlier done row while a newer ending is still being written.
                if index == len(lines) - 1 and not body.endswith("\n"):
                    raise ValueError("incomplete task journal append")
                raise
            if not isinstance(row, dict) or not isinstance(row.get("id"), str):
                raise ValueError("task journal row has no string id")
            latest[(row.get("sessionId", ""), str(row["id"]))] = row
    return list(latest.values())


def successful(home, ids):
    rows = outcomes(home)
    for task_id in ids:
        matches = [row for row in rows if row["id"] == task_id]
        if len(matches) != 1 or matches[0].get("status") != "done":
            return False
    return True


def snapshot(home, output, known):
    output.mkdir(parents=True, exist_ok=True, mode=0o700)
    errors = []
    retained = []
    for path in owned_files(home):
        relative = path.relative_to(home)
        if path.suffix in {".json", ".jsonl", ".log", ".md", ".txt"}:
            try:
                # A diagnostic must not accidentally copy an unbounded tool
                # artifact. Report its omission alongside the durable outcome.
                if path.stat().st_size > 16 * 1024 * 1024:
                    errors.append({"file": str(relative), "error": "exceeds sixteen-MiB evidence bound"})
                    continue
                text = path.read_text(errors="replace")
                target = output / "records" / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text(redact(text, known))
                retained.append(str(relative))
            except OSError as error:
                errors.append({"file": str(relative), "error": str(error)})
        elif re.fullmatch(r"plandb\.db(?:\.\d+)?", path.name):
            connection = None
            try:
                connection = sqlite3.connect(path.resolve().as_uri() + "?mode=ro", uri=True, timeout=1)
                connection.row_factory = sqlite3.Row
                rows = [dict(row) for row in connection.execute("SELECT * FROM tasks")]
                target = output / "sqlite" / Path(str(relative) + ".json")
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text(redact(json.dumps(rows, indent=2), known))
                retained.append(str(relative))
            except (OSError, sqlite3.Error) as error:
                errors.append({"file": str(relative), "error": str(error)})
            finally:
                if connection:
                    connection.close()
    try:
        rows = outcomes(home)
    except (OSError, ValueError, KeyError) as error:
        rows = []
        errors.append({"file": "task outcomes", "error": str(error)})
    summary = {"outcomes": rows, "retained": retained, "errors": errors}
    (output / "summary.json").write_text(redact(json.dumps(summary, indent=2), known))
    return summary


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=["terminal", "snapshot", "redact"])
    parser.add_argument("--home", type=Path)
    parser.add_argument("--ids", nargs="+", default=[])
    parser.add_argument("--output", type=Path)
    parser.add_argument("--copied-config")
    args = parser.parse_args()
    if args.mode == "redact":
        sys.stdout.write(redact(sys.stdin.read(), secrets(args.copied_config)))
    elif args.mode == "terminal":
        if not args.home or not args.ids:
            parser.error("terminal needs --home and --ids")
        try:
            return 0 if successful(args.home, args.ids) else 1
        except (OSError, ValueError, KeyError):
            return 1
    else:
        if not args.home or not args.output:
            parser.error("snapshot needs --home and --output")
        snapshot(args.home, args.output, secrets(args.copied_config))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

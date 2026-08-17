#!/usr/bin/env python3
"""summarize.py — turn a wire log and a phase file into the table.

    python3 perf-report/ssh/summarize.py wire.log phases.txt [--seconds]

The wire log is what internal/wirelog writes: one line per wall-clock second,
`unix_ms bytes writes total_bytes`, with silent seconds present as zeros. The
phase file is what measure.sh writes: `unix_seconds label`, one per boundary,
ending with `done`.

Every number printed is a count of bytes the surface actually handed to the
terminal. Nothing here is sampled, averaged over runs, or modelled.
"""

import sys


def read_log(path):
    """(second, bytes, writes) per row, header and blanks dropped."""
    rows = []
    with open(path) as handle:
        for line in handle:
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            ms, count, writes, _total = (int(field) for field in line.split())
            rows.append((ms // 1000, count, writes))
    return rows


def read_phases(path):
    """[(label, start, end)] — the last mark closes the one before it."""
    marks = []
    with open(path) as handle:
        for line in handle:
            line = line.strip()
            if not line:
                continue
            when, label = line.split(None, 1)
            marks.append((int(when), label))
    return [
        (label, start, marks[i + 1][0])
        for i, (start, label) in enumerate(marks[:-1])
    ]


def summarize(label, rows):
    seconds = len(rows)
    # Seconds in which anything at all was drawn. A phase's mean over the whole
    # window answers "what did this cost the link"; the mean over the live
    # seconds answers "what does drawing cost", and the two differ by however
    # long the model kept the surface waiting — which is not a property of the
    # surface.
    live = sum(1 for _, count, _ in rows if count)
    total = sum(count for _, count, _ in rows)
    writes = sum(w for _, _, w in rows)
    peak = max((count for _, count, _ in rows), default=0)
    return {
        "phase": label,
        "s": seconds,
        "live": live,
        "bytes": total,
        "B/s": total / seconds if seconds else 0.0,
        "live B/s": total / live if live else 0.0,
        "peak B/s": peak,
        "writes/s": writes / seconds if seconds else 0.0,
        "B/write": total / writes if writes else 0.0,
    }


COLUMNS = [
    "phase",
    "s",
    "live",
    "bytes",
    "B/s",
    "live B/s",
    "peak B/s",
    "writes/s",
    "B/write",
]


def render(table):
    def cell(row, key):
        value = row[key]
        if isinstance(value, float):
            return f"{value:.1f}"
        return f"{value:,}" if isinstance(value, int) and key == "bytes" else str(value)

    widths = {
        key: max(len(key), max((len(cell(row, key)) for row in table), default=0))
        for key in COLUMNS
    }
    lines = ["  ".join(key.rjust(widths[key]) for key in COLUMNS)]
    lines.append("  ".join("-" * widths[key] for key in COLUMNS))
    for row in table:
        lines.append("  ".join(cell(row, key).rjust(widths[key]) for key in COLUMNS))
    return "\n".join(lines)


def main(argv):
    if len(argv) < 3:
        print(__doc__, file=sys.stderr)
        return 2
    rows = read_log(argv[1])
    phases = read_phases(argv[2])
    if not rows:
        print("wire log is empty: did the surface open?", file=sys.stderr)
        return 1

    table = []
    for label, start, end in phases:
        window = [row for row in rows if start <= row[0] < end]
        if window:
            table.append(summarize(label, window))
    table.append(summarize("ALL", rows))
    print(render(table))

    idle = [count for second, count, _ in rows if count == 0]
    print(
        f"\n{len(idle)} of {len(rows)} seconds cost nothing at all. "
        f"{sum(c for _, c, _ in rows):,} bytes total, "
        f"{sum(w for _, _, w in rows):,} writes."
    )

    if "--seconds" in argv[3:]:
        print("\nper second (unix_s, bytes, writes):")
        boundaries = {start: label for label, start, _ in phases}
        for second, count, writes in rows:
            edge = f"  <- {boundaries[second]}" if second in boundaries else ""
            print(f"{second} {count:8d} {writes:5d}{edge}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))

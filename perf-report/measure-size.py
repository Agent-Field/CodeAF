#!/usr/bin/env python3
"""Binary size attribution for the aforge binary.

Two views, because neither alone is honest:

  sections   what the file is made of.  `-s -w` strips the symbol table and
             DWARF, so what remains is .text, .gopclntab (the PC->line table
             the runtime needs for tracebacks — it shrinks only when fewer
             functions are linked, never from a flag) and .rodata.
  packages   symbol sizes bucketed by package prefix, read from an UNSTRIPPED
             build because a stripped one has no symbol names left.

BSS is dropped from the package totals, and the reason is one symbol:
crypto/internal/fips140/drbg.memory is 32 MiB of zeroes that cost address
space and not a single file byte.  Every naive "top symbols" list of this
binary leads with it and is wrong.  Note that `go tool nm` files it under D;
binutils `nm -S` gets its section right, which is why that is the one used.

usage: measure-size.py <stripped-binary> <unstripped-binary> [--json out.json]
"""

import collections
import json
import os
import subprocess
import sys


def sections(binary):
    out = subprocess.run(["size", "-A", binary], capture_output=True, text=True)
    rows = {}
    for line in out.stdout.splitlines():
        parts = line.split()
        if len(parts) >= 2 and parts[0].startswith("."):
            try:
                rows[parts[0]] = int(parts[1])
            except ValueError:
                pass
    return rows


def bucket(symbol):
    """Fold a Go symbol name down to the package that owns it."""
    for marker in ("go:", "type:", "gclocals", "runtime."):
        if symbol.startswith(marker):
            return marker.rstrip(".")
    name = symbol.split("[")[0]
    cut = name.rfind("/")
    head = name[: cut + 1] if cut >= 0 else ""
    tail = name[cut + 1:]
    return head + tail.split(".")[0] if "." in tail else name


def packages(binary):
    out = subprocess.run(["nm", "-S", binary], capture_output=True, text=True)
    totals = collections.Counter()
    counts = collections.Counter()
    for line in out.stdout.splitlines():
        parts = line.split()
        if len(parts) < 4:
            continue
        try:
            # binutils prints addresses and sizes in hex by default.
            size = int(parts[1], 16)
        except ValueError:
            continue
        kind, symbol = parts[2], parts[3]
        # B/b is BSS — .bss and .noptrbss both — zeroed at load and not
        # stored in the file at all.
        if kind in ("B", "b"):
            continue
        key = bucket(symbol)
        totals[key] += size
        counts[key] += 1
    return totals, counts


def main():
    args = [a for a in sys.argv[1:] if not a.startswith("--")]
    stripped, unstripped = args[0], args[1]

    sec = sections(stripped)
    totals, counts = packages(unstripped)
    report = {
        "stripped_bytes": os.path.getsize(stripped),
        "unstripped_bytes": os.path.getsize(unstripped),
        "sections": sec,
        "top_packages": [
            {"pkg": k, "bytes": v, "symbols": counts[k]}
            for k, v in totals.most_common(30)
        ],
    }
    if "--json" in sys.argv:
        with open(sys.argv[sys.argv.index("--json") + 1], "w") as fh:
            json.dump(report, fh, indent=2)

    print(f"stripped    {report['stripped_bytes'] / 1e6:.2f} MB")
    print(f"unstripped  {report['unstripped_bytes'] / 1e6:.2f} MB")
    print("sections (stripped):")
    for name, size in sorted(sec.items(), key=lambda kv: -kv[1])[:8]:
        print(f"  {size / 1e6:7.2f} MB  {name}")
    print("top packages by symbol bytes (unstripped, BSS excluded):")
    for row in report["top_packages"][:20]:
        print(f"  {row['bytes'] / 1e6:7.3f} MB  {row['symbols']:6d} syms  {row['pkg']}")


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""Startup latency + init-footprint harness for the aforge binary.

Two numbers, both reproducible:

  wall    median/p10 of process lifetime for `aforge --help`, which runs every
          package init in the binary and then exits.  This is the floor under
          every command, `chat` included.
  init    GODEBUG=inittrace=1 totals (ms of init clock, bytes, allocs).  These
          are exact and immune to machine noise, which is why a fix is argued
          from them and confirmed by the wall clock.

usage: measure-startup.py <binary> [reps] [--json out.json]
"""

import json
import os
import re
import statistics
import subprocess
import sys
import time

INIT_LINE = re.compile(
    r"init (\S+) @[\d.]+ ms, ([\d.]+) ms clock, (\d+) bytes, (\d+) allocs"
)


def wall(binary, reps):
    # One warm run so the page cache and the dynamic loader are not in the
    # sample.  A 44MB binary otherwise pays its first read into the median.
    subprocess.run([binary, "--help"], stdout=subprocess.DEVNULL,
                   stderr=subprocess.DEVNULL)
    out = []
    for _ in range(reps):
        t0 = time.perf_counter()
        subprocess.run([binary, "--help"], stdout=subprocess.DEVNULL,
                       stderr=subprocess.DEVNULL)
        out.append((time.perf_counter() - t0) * 1000.0)
    out.sort()
    return {
        "reps": reps,
        "min_ms": round(out[0], 3),
        "p10_ms": round(out[len(out) // 10], 3),
        "median_ms": round(statistics.median(out), 3),
        "mean_ms": round(statistics.mean(out), 3),
    }


def maxrss(binary):
    """Peak RSS of the init-only run, in KiB, via wait4 accounting."""
    pid = os.fork()
    if pid == 0:
        devnull = os.open(os.devnull, os.O_WRONLY)
        os.dup2(devnull, 1)
        os.dup2(devnull, 2)
        os.execv(binary, [binary, "--help"])
        os._exit(127)
    _, _, usage = os.wait4(pid, 0)
    return usage.ru_maxrss


def inittrace(binary):
    proc = subprocess.run(
        [binary, "--help"],
        env={**os.environ, "GODEBUG": "inittrace=1"},
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
        text=True,
    )
    packages, ms, byt, allocs = 0, 0.0, 0, 0
    per_pkg = []
    for line in proc.stderr.splitlines():
        m = INIT_LINE.match(line)
        if not m:
            continue
        packages += 1
        ms += float(m.group(2))
        byt += int(m.group(3))
        allocs += int(m.group(4))
        per_pkg.append((float(m.group(2)), int(m.group(3)), int(m.group(4)),
                        m.group(1)))
    per_pkg.sort(reverse=True)
    return {
        "packages": packages,
        "init_ms": round(ms, 2),
        "init_bytes": byt,
        "init_allocs": allocs,
        "top": [{"pkg": p, "ms": a, "bytes": b, "allocs": c}
                for a, b, c, p in per_pkg[:15]],
    }


def main():
    args = [a for a in sys.argv[1:] if not a.startswith("--")]
    binary = args[0] if args else "bin/aforge"
    reps = int(args[1]) if len(args) > 1 else 30

    report = {
        "binary": os.path.abspath(binary),
        "binary_bytes": os.path.getsize(binary),
        "wall": wall(binary, reps),
        "maxrss_kib": maxrss(binary),
        "init": inittrace(binary),
    }

    if "--json" in sys.argv:
        dest = sys.argv[sys.argv.index("--json") + 1]
        with open(dest, "w") as fh:
            json.dump(report, fh, indent=2)

    w, i = report["wall"], report["init"]
    print(f"binary      {report['binary_bytes'] / 1e6:.1f} MB")
    print(f"wall        median {w['median_ms']:.2f} ms  "
          f"p10 {w['p10_ms']:.2f} ms  min {w['min_ms']:.2f} ms  "
          f"(n={w['reps']})")
    print(f"maxrss      {report['maxrss_kib'] / 1024:.1f} MiB")
    print(f"init        {i['init_ms']:.2f} ms  "
          f"{i['init_bytes'] / 1e6:.2f} MB  "
          f"{i['init_allocs']} allocs  over {i['packages']} packages")
    print("top init packages:")
    for row in i["top"][:8]:
        print(f"  {row['ms']:6.2f} ms {row['bytes'] / 1e6:6.2f} MB "
              f"{row['allocs']:7d}  {row['pkg']}")


if __name__ == "__main__":
    main()

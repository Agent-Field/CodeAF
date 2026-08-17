#!/usr/bin/env python3
"""Time from exec of `aforge chat` to its first painted byte.

`chat` is a full-screen bubbletea program, so it only runs against a terminal.
The harness gives it one: a pty pair, a fresh throwaway --db so no journal
replay is in the sample, and a stopwatch that stops on the first byte the
program writes.  Then it sends the quit key and reaps.

The measurement is deliberately "first byte", not "first complete frame":
bubbletea's opening write is the alt-screen enter plus the first View(), so the
first byte lands only after Init() and the first render have both run.

usage: measure-firstpaint.py <binary> [reps]
"""

import os
import pty
import shutil
import statistics
import subprocess
import sys
import tempfile
import time


def one(binary, workdir):
    db = os.path.join(workdir, f"chat-{time.time_ns()}.db")
    primary, replica = pty.openpty()
    os.set_blocking(primary, False)
    t0 = time.perf_counter()
    proc = subprocess.Popen(
        [binary, "chat", "--db", db, "--session", "new"],
        stdin=replica, stdout=replica, stderr=replica,
        close_fds=True, start_new_session=True,
        env={**os.environ, "TERM": "xterm-256color"},
    )
    os.close(replica)

    elapsed = None
    deadline = t0 + 30.0
    while time.perf_counter() < deadline:
        try:
            if os.read(primary, 4096):
                elapsed = (time.perf_counter() - t0) * 1000.0
                break
        except BlockingIOError:
            time.sleep(0.0002)
        except OSError:
            break

    proc.terminate()
    try:
        proc.wait(timeout=5)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait(timeout=5)
    os.close(primary)
    return elapsed


def main():
    binary = sys.argv[1] if len(sys.argv) > 1 else "bin/aforge"
    reps = int(sys.argv[2]) if len(sys.argv) > 2 else 15
    workdir = tempfile.mkdtemp(prefix="aforge-firstpaint-")
    try:
        one(binary, workdir)  # warm
        samples = [s for s in (one(binary, workdir) for _ in range(reps))
                   if s is not None]
    finally:
        shutil.rmtree(workdir, ignore_errors=True)

    if not samples:
        print("first paint: no output captured")
        return 1
    samples.sort()
    print(f"first paint  median {statistics.median(samples):.2f} ms  "
          f"min {samples[0]:.2f} ms  max {samples[-1]:.2f} ms  "
          f"(n={len(samples)}/{reps})")
    return 0


if __name__ == "__main__":
    sys.exit(main())

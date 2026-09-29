#!/usr/bin/env python3
"""A `timeout` for hosts without coreutils: run a command with a wall, kill
its whole process group when the wall fires, and exit 124 — the same code
GNU timeout answers, because that is what the rig's logs and callers assume.

    timeout.py <seconds> <command> [args...]
"""

import os
import signal
import subprocess
import sys


def main():
    if len(sys.argv) < 3:
        sys.exit("usage: timeout.py <seconds> <command> [args...]")
    seconds = float(sys.argv[1])
    command = sys.argv[2:]
    try:
        process = subprocess.Popen(command, start_new_session=True)
    except FileNotFoundError as e:
        print(f"timeout.py: {e}", file=sys.stderr)
        sys.exit(127)
    try:
        code = process.wait(timeout=seconds)
    except subprocess.TimeoutExpired:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        process.wait()
        print(f"timeout.py: wall of {seconds:g}s fired", file=sys.stderr)
        sys.exit(124)
    sys.exit(code)


if __name__ == "__main__":
    main()
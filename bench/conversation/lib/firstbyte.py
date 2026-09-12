#!/usr/bin/env python3
"""Copy a harness's stdout to a file and stamp the moment it first says anything.

WALL-TO-DONE IS NOT WALL-TO-FIRST-WORD, and a comparison that carries only the
first number cannot see the difference between a harness that thinks for ninety
seconds and then writes, and one that starts writing at three seconds and takes
the same ninety to finish. The second is the one people describe as fast. So the
print door streams through here: every byte still lands in the log exactly as it
arrived, and the offset of the first one is written beside it.

The clock is the caller's. It passes `--started` as a `time.monotonic()` reading
taken immediately before the child was launched, so the stamp is an offset in
the same clock the cell's wall time is measured in, and the two are subtractable.

Bytes are copied unbuffered and never decoded: a harness that emits a partial
UTF-8 sequence, an escape run, or a NUL must reach the log byte for byte, and a
stamper that tried to read lines would also change WHEN the first byte appeared
by waiting for a newline that a streaming reply has not written yet.
"""
import argparse
import sys
import time


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", required=True, help="where the copied stream lands")
    parser.add_argument("--stamp", required=True, help="where the first-byte offset is written")
    parser.add_argument("--started", type=float, required=True,
                        help="time.monotonic() taken just before the child started")
    args = parser.parse_args()

    first = None
    with open(args.out, "wb") as out:
        source = sys.stdin.buffer
        while True:
            chunk = source.read1(65536) if hasattr(source, "read1") else source.read(65536)
            if not chunk:
                break
            if first is None:
                first = time.monotonic() - args.started
            out.write(chunk)
            out.flush()
    # A harness that said nothing leaves no stamp at all rather than a zero:
    # "it never spoke" and "it spoke instantly" are opposite findings and the
    # emptiness law says the unknown one draws nothing.
    if first is not None:
        with open(args.stamp, "w") as stamp:
            stamp.write("%.3f\n" % max(first, 0.0))
    return 0


if __name__ == "__main__":
    sys.exit(main())

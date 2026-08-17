#!/usr/bin/env python3
"""probe2026.py — does the surface wrap its frames in synchronized output?

    python3 perf-report/ssh/probe2026.py            bin/aforge chat
    python3 perf-report/ssh/probe2026.py --silent   bin/aforge chat

Runs the command on a 130x40 pty this script owns, so it can see every byte the
program writes AND answer the terminal's half of the conversation.

DEC mode 2026 (synchronized output, "BSU/ESU") is a promise from the terminal
that it will not paint half a frame: the program brackets an update with
`CSI ? 2026 h` ... `CSI ? 2026 l` and the terminal shows all of it or none of
it. Bubble Tea only makes that promise if the terminal answers a DECRQM query
saying it understands the mode. The question this script answers is which side
is missing when the brackets are absent.

By default the probe plays a terminal that supports the mode: it watches for
`CSI ? 2026 $ p` and replies `CSI ? 2026 ; 2 $ y` (recognized, currently reset).
With --silent it stays quiet, like a terminal that does not implement DECRQM.
Run it both ways: brackets in the first and none in the second means the
program is wired correctly and it is the terminal that is silent.

The probe types a few characters into the surface before it judges, because an
idle aforge draws nothing at all — with no frames there is nothing to wrap, and
a verdict taken over an idle second would be a verdict about nothing.

Exit status is 0 when frames were wrapped, 1 when they were not.
"""

import fcntl
import os
import pty
import select
import struct
import sys
import termios
import time

QUERY_2026 = b"\x1b[?2026$p"
QUERY_2027 = b"\x1b[?2027$p"
REPLY_2026 = b"\x1b[?2026;2$y"  # recognized, reset — "supported, currently off"
REPLY_2027 = b"\x1b[?2027;2$y"
BSU = b"\x1b[?2026h"
ESU = b"\x1b[?2026l"

# The rest of the handshake a terminal owes a program at startup. None of it is
# about mode 2026, and all of it has to be answered anyway: a program that asks
# where the cursor is and never finds out waits, and a probe that waits is
# measuring its own silence.
CIVILITIES = {
    b"\x1b]11;?\x1b\\": b"\x1b]11;rgb:0000/0000/0000\x1b\\",  # background color
    b"\x1b[6n": b"\x1b[1;1R",  # cursor position
    b"\x1b[c": b"\x1b[?1;2c",  # device attributes
}

ROWS, COLS = 40, 130


class Probe:
    """A terminal, in as much detail as this question needs.

    It answers what a real one would, and mode 2026 only when it is playing a
    terminal that supports it.
    """

    def __init__(self, fd, answer):
        self.fd = fd
        self.answer = answer
        self.seen = bytearray()
        self.replied = False
        self.said = set()

    def pump(self, seconds):
        """Read whatever the program says for a while, answering as we go."""
        until = time.monotonic() + seconds
        while time.monotonic() < until:
            ready, _, _ = select.select([self.fd], [], [], 0.1)
            if not ready:
                continue
            try:
                chunk = os.read(self.fd, 65536)
            except OSError:
                return False
            if not chunk:
                return False
            self.seen += chunk
            self.reply()
        return True

    def reply(self):
        """Answer every query we recognize, once each."""
        for query, answer in CIVILITIES.items():
            if query in self.seen and query not in self.said:
                os.write(self.fd, answer)
                self.said.add(query)
        if self.answer and not self.replied and QUERY_2026 in self.seen:
            os.write(self.fd, REPLY_2026 + (REPLY_2027 if QUERY_2027 in self.seen else b""))
            self.replied = True

    def mark(self):
        """Where the transcript is now, so a later count can start here."""
        return len(self.seen)


def main(argv):
    answer = "--silent" not in argv
    command = [arg for arg in argv[1:] if arg != "--silent"]
    if not command:
        print(__doc__, file=sys.stderr)
        return 2

    pid, fd = pty.fork()
    if pid == 0:
        # The child waits a moment so the parent can size the pty before the
        # program asks how big it is.
        os.execvp(
            "/bin/sh", ["/bin/sh", "-c", 'sleep 0.5; exec "$@"', "sh"] + command
        )

    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
    probe = Probe(fd, answer)

    probe.pump(3.0)  # boot, the query, and the first full paint
    after_boot = probe.mark()
    for letter in b"synchronized":  # typing, which is the cheapest way to draw
        os.write(fd, bytes([letter]))
        probe.pump(0.2)
    probe.pump(1.0)
    typed = bytes(probe.seen[after_boot:])

    os.write(fd, b"\x03")
    probe.pump(0.5)
    os.close(fd)
    try:
        os.waitpid(pid, os.WNOHANG)
    except (ChildProcessError, OSError):
        pass

    asked = probe.seen.count(QUERY_2026)
    starts, ends = typed.count(BSU), typed.count(ESU)
    # The four variables Bubble Tea's shouldQuerySynchronizedOutput reads. They
    # decide the whole verdict, so the run prints the ones it actually had
    # rather than the ones whoever ran it meant to set.
    print(
        "environment:    "
        + ", ".join(
            f"{name}={os.environ.get(name) or '-'}"
            for name in ("TERM", "TERM_PROGRAM", "SSH_TTY", "WT_SESSION")
        )
    )
    print(f"terminal role:  {'answers DECRQM 2026' if answer else 'silent (no DECRQM)'}")
    print(f"bytes read:     {len(probe.seen):,} ({len(typed):,} while typing)")
    print(f"program asked:  {asked}  (CSI ? 2026 $ p)")
    print(f"probe replied:  {'yes' if probe.replied else 'no'}")
    print(f"frames wrapped: BSU {starts}, ESU {ends}")
    if starts and ends:
        print("VERDICT: synchronized output is engaged — frames are atomic.")
        return 0
    if not asked:
        print("VERDICT: the program never asked. Nothing the terminal does can help.")
        # Either the program is in an SSH session (where Bubble Tea skips the
        # query on purpose) or it never got as far as drawing. The first bytes
        # tell those two apart at a glance.
        print(f"first bytes:    {bytes(probe.seen[:120])!r}")
    else:
        print("VERDICT: asked, unwrapped. The terminal's answer is what is missing.")
    return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv))

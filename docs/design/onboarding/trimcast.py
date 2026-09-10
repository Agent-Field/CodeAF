#!/usr/bin/env python3
"""Cut an asciinema recording where the application gives the screen back.

aforge draws on the alternate screen. Everything after it switches back — a
shutdown log line, the shell prompt behind it — belongs to the terminal the
recording was started from and not to the journey being shown, and a GIF whose
last frame is somebody's shell is a GIF that ends on a mistake.

This trims the tail at that switch and does nothing else: no event is edited,
reordered, resampled or invented, and everything before the switch is the
recording exactly as it was captured.
"""

import json
import sys

# The escape sequence that leaves the alternate screen buffer.
LEAVE_ALT_SCREEN = "\x1b[?1049l"


def main(path: str) -> None:
    lines = open(path).read().splitlines()
    if not lines:
        return
    kept = [lines[0]]  # the header, which carries the recorded width and height
    for line in lines[1:]:
        event = json.loads(line)
        if event[1] == "o" and LEAVE_ALT_SCREEN in event[2]:
            break
        kept.append(line)
    open(path, "w").write("\n".join(kept) + "\n")


if __name__ == "__main__":
    main(sys.argv[1])

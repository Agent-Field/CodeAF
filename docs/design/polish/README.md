# The polish wave

`scripts/frame.sh` drives the real `bin/aforge` in a real terminal on a private
tmux socket and writes what the screen actually held. Every ledger row in
`LEDGER.md` closes on a captured pair of those frames — before and after — and
never on an argument about what the code ought to draw.

```sh
make build
go build -o bin/aforge-demo-home ./cmd/aforge-demo-home
./bin/aforge-demo-home --into /tmp/demohome        # something on every place
export DEMO_HOME=/tmp/demohome
scripts/frame.sh home 120 40                       # → frames/home.120x40.txt
scripts/frame.sh tasks 80 24 Tab                   # keys are sent in order
```

The four sizes every surface is judged at, and why:

| Size | What it is |
| --- | --- |
| `160x50` | a 16" MacBook, full screen — the most room anyone has |
| `120x40` | the ordinary window, and the size most of this was found at |
| `80x24` | the floor a terminal is allowed to be |
| `60x30` | the phone tier — a split pane, an ssh session from a train |

`.txt` is the plain frame, `.ans` the same frame with its colour, which is what
the theme rows are read from.

## Naming a frame, and why it is worth a rule

A ledger row closes on a captured before/after pair and on nothing else, so the
pairing has to be mechanical — and it drifted. Twelve lanes captured frames and
each invented its own convention: `chat-md` against `chat2-md`, `home-idle`
against `home2`, `keep-taskpage` for a verification pass. `gallery.py` knew only
one of them, so it reported 38 pairs against 160 loose frames and made a wave
that HAD closed its rows on captures look as though it had not. It reads all
three now, because renaming a checked-in frame would break the link in a ledger
row that already cites it.

**A new lane uses one form:**

```
<row>-before.<w>x<h>.txt      <row>-after.<w>x<h>.txt
```

Both captured by the lane that fixes the row, in the same pass. It is the only
form that does not depend on an older file still being where somebody left it,
and it is the one the task-page lane used to close the first defect this wave
found. Capture at 160x50, 120x40, 80x24 and 60x30 unless the row is about one
width, and say which widths in the audit.

A frame captured once is not a gap: most of them are the exploratory captures an
audit was WRITTEN from — a stream walked turn by turn, a resize taken one step
at a time, one key probed. The gallery lists those separately and says so.

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

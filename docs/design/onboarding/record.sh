#!/usr/bin/env bash
# Record the onboarding journey from the REAL bin/aforge, in a real 120x24
# terminal, and render it to a GIF.
#
# It is a recording of the real binary and nothing else: no mock, no
# reconstructed frames, no edited frames. The one thing it arranges is the WORLD
# the binary runs in — a throwaway home, a throwaway project, and a fixture
# credential of the right shape that no request is ever made with. The setup
# checks a pasted key for shape only and calls no model, so the whole recording
# costs nothing and reaches no network.
#
#   ./docs/design/onboarding/record.sh
#
# Needs: tmux, asciinema, agg, ffmpeg (for the stills). Writes
# docs/design/onboarding/onboarding-wide.gif and leaves the cast under $OUT.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
BIN="$REPO/bin/aforge"
OUT="${OUT:-/tmp/onb-rec}"
LAB="${LAB:-$OUT/home/src/parser}"
SESSION="${SESSION:-onbrec}"
GIF="${GIF:-$REPO/docs/design/onboarding/onboarding-wide.gif}"

# THE CREDENTIAL IS A FIXTURE AND IS NEVER SENT ANYWHERE. internal/config's
# LooksLikeAPIKey checks the shape — `sk-` and twenty characters — and the setup
# makes no call with it, so this is the whole of what the connection step needs
# to be shown working. No real key goes near this script.
FIXTURE_KEY="sk-or-v1-demo0000000000000000000000000000"

[ -x "$BIN" ] || { echo "build first: make build" >&2; exit 1; }
for tool in tmux asciinema agg python3; do
  command -v "$tool" >/dev/null || { echo "missing $tool" >&2; exit 1; }
done

# A THROWAWAY HOME AND A THROWAWAY PROJECT. Nothing here touches ~/.aforge.
#
# IT IS A REAL PROJECT AND NOT AN EMPTY DIRECTORY, because those are two
# different screens: bare `aforge` outside a project opens a workspace of its own
# and the first conversation says so, and inside one it names the folder. The
# recording shows the ordinary case.
rm -rf "$OUT/home"
mkdir -p "$LAB"
cat > "$LAB/README.md" <<'PROJECT'
# parser
A small example project used for the onboarding recording.
PROJECT
git -C "$LAB" init -q
git -C "$LAB" add README.md
git -C "$LAB" -c user.email=nobody@example.invalid -c user.name=nobody \
  commit -qm "the example project"

tmux kill-session -t "$SESSION" 2>/dev/null || true
# The tmux window is a little larger than the recording so nothing wraps in the
# pane; the pty asciinema opens is exactly 120x24 and that is what is recorded.
tmux -f /dev/null new-session -d -s "$SESSION" -c "$LAB" \
  "asciinema rec --cols 120 --rows 24 --overwrite --quiet -c \
   'env AFORGE_HOME=$OUT/home HOME=$OUT/home OPENROUTER_API_KEY= OPENAI_API_KEY= $BIN chat --no-host' \
   $OUT/onboarding.cast"
tmux resize-window -t "$SESSION" -x 130 -y 32

say() { tmux send-keys -t "$SESSION" "$@"; }
beat() { sleep "${1:-1.2}"; }

beat 3.5                                   # the connection screen settles

# ── connect ──
say -l "$FIXTURE_KEY"; beat 2.0            # the key, masked as it lands
say Enter; beat 4.0                        # the controls screen, and the panel plays

# ── the day's limit ──
say -l "25"; beat 2.2                      # an amount typed; the panel settles at once
say Enter; beat 2.0                        # committed, focus walks to the model

# ── the chat model, and the panel beside it ──
say Enter; beat 2.0                        # the catalog list opens
say Down; beat 1.0
say Down; beat 1.5                         # it scrolls past five rows
say Escape; beat 1.8                       # cancelling keeps the model it had

# ── the work crew, and the /task demonstration ──
say Tab; beat 3.6                          # the panel follows the focus and plays once
say Enter; beat 2.2                        # the three presets, each explained
say Down; beat 1.2
say Enter; beat 2.0                        # Max taken
say -l "?"; beat 2.5                       # the seats, on demand
say -l "?"; beat 1.0

# ── browsing the examples is the other deliberate act ──
say Right; beat 3.4                        # a fourth example, played once and still
say Left; beat 2.6                         # and back

# ── the settings it does not ask about ──
say Tab; beat 1.2
say Enter; beat 2.8                        # memory, permissions, the countdown
say Escape; beat 1.0

# ── the way out, and the first conversation ──
say Tab; beat 1.5
say Enter; beat 4.0                        # What would you like to work on?
say Down; beat 2.5                         # a starting point, and its helper
say Enter; beat 4.0                        # it fills the box and sends nothing

# Leave. ctrl+c twice is the door, here as everywhere.
say C-c; sleep 0.4; say C-c; sleep 2

tmux kill-session -t "$SESSION" 2>/dev/null || true

# THE RECORDING STOPS WHERE THE APPLICATION DOES. Everything after aforge gives
# the screen back is the shell it was started from — a log line, a prompt — and
# it is cut rather than shown as though it were part of the journey. This is a
# trim of the tail: no frame is edited, reordered or invented.
python3 "$REPO/docs/design/onboarding/trimcast.py" "$OUT/onboarding.cast"

agg --font-size 13 --fps-cap 10 --idle-time-limit 1.2 \
    --font-family "DejaVu Sans Mono" \
    "$OUT/onboarding.cast" "$GIF"
ls -lh "$GIF"

# A REAL NARROW TERMINAL, RECORDED THE SAME WAY. The wide GIF cannot show what a
# forty-column window does with this screen, and "it still works narrow" is a
# claim that has to be looked at rather than asserted. This is one still of the
# real binary at 40x16 with the crew chooser open — the state that has the most
# to fit — written beside the GIF.
NARROW="${NARROW:-$REPO/docs/design/onboarding/onboarding-40x16.png}"
tmux kill-session -t "$SESSION-narrow" 2>/dev/null || true
tmux -f /dev/null new-session -d -s "$SESSION-narrow" -c "$LAB" \
  "asciinema rec --cols 40 --rows 16 --overwrite --quiet -c \
   'env AFORGE_HOME=$OUT/home-narrow HOME=$OUT/home-narrow OPENROUTER_API_KEY= OPENAI_API_KEY= $BIN chat --no-host' \
   $OUT/narrow.cast"
tmux resize-window -t "$SESSION-narrow" -x 60 -y 24
sayn() { tmux send-keys -t "$SESSION-narrow" "$@"; }
sleep 3.5
sayn -l "$FIXTURE_KEY"; sleep 1.5
sayn Enter; sleep 2.5
sayn Tab; sleep 0.6
sayn Tab; sleep 0.6                        # onto the crew
sayn Enter; sleep 2.0                      # the three presets, whole, at forty columns
sayn C-c; sleep 0.4; sayn C-c; sleep 2
tmux kill-session -t "$SESSION-narrow" 2>/dev/null || true
python3 "$REPO/docs/design/onboarding/trimcast.py" "$OUT/narrow.cast"
agg --font-size 15 --fps-cap 5 --idle-time-limit 1.0 \
    --font-family "DejaVu Sans Mono" \
    "$OUT/narrow.cast" "$OUT/narrow.gif"
# The last frame of that recording is the still: the chooser open, at 40x16.
ffmpeg -y -loglevel error -sseof -0.4 -i "$OUT/narrow.gif" -update 1 -frames:v 1 "$NARROW"
ls -lh "$NARROW"

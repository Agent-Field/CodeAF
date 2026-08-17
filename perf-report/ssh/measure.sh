#!/usr/bin/env bash
#
# measure.sh — a scripted minute of aforge chat with the byte meter running.
#
#   perf-report/ssh/measure.sh local [outdir]
#   perf-report/ssh/measure.sh ssh   [outdir]
#
# Opens the v3 surface in a fresh 130x40 tmux session with AFORGE_WIRE_LOG set,
# drives it through the same six phases every time — boot, idle, typing, the
# wait for the first token, the streaming reply, and the quiet after it — and
# then hands the log and the phase marks to summarize.py.
#
# `local` runs the binary in the tmux pane: the app's terminal is tmux, and the
# bytes the meter counts are the bytes tmux receives.
#
# `ssh` runs it through `ssh -t localhost` from inside that same pane, so the
# app has a real SSH pty with SSH_TTY set and the bytes the meter counts are
# the bytes that cross the link. That is the number the SSH story is about, and
# it is also why the two modes are not directly comparable line by line: they
# measure the same surface talking to two different listeners.
#
# Needs: tmux, an OPENROUTER_API_KEY in the environment, and for `ssh` mode a
# working `ssh localhost` (key auth, no prompt).
set -euo pipefail

MODE="${1:-local}"
case "$MODE" in
local | ssh) ;;
*)
	echo "usage: $0 local|ssh [outdir]" >&2
	exit 2
	;;
esac

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT="${2:-$(mktemp -d -t aforge-wire-XXXXXX)}"
mkdir -p "$OUT"
WIRE="$OUT/wire-$MODE.log"
PHASES="$OUT/phases-$MODE.txt"
: >"$PHASES"
rm -f "$WIRE"

if [[ -z "${OPENROUTER_API_KEY:-}" ]]; then
	echo "$0: OPENROUTER_API_KEY is not set; the streaming phase needs a model that answers" >&2
	exit 2
fi

# A scratch workspace, so a measurement run never leaves session files, history
# or drafts in a directory anyone works in.
WS="$(mktemp -d -t aforge-wire-ws-XXXXXX)"
# The key goes in a file the harness owns rather than on the command line: an
# `ssh host CMD` argument is world-readable in ps for as long as it runs.
ENVFILE="$WS/.env"
umask 077
cat >"$ENVFILE" <<EOF
OPENROUTER_API_KEY=$OPENROUTER_API_KEY
AFORGE_WIRE_LOG=$WIRE
EOF

SESSION="aforge-wire-$MODE-$$"
# On any exit, wanted or not: no stray session, and no key left in /tmp.
cleanup() {
	tmux kill-session -t "$SESSION" 2>/dev/null || true
	rm -rf "$WS"
}
trap cleanup EXIT

mark() { echo "$(date +%s) $1" >>"$PHASES"; }
type_slowly() { # one character at a time, about twelve a second, like a person
	local text="$1" i
	for ((i = 0; i < ${#text}; i++)); do
		tmux send-keys -t "$SESSION" -l -- "${text:i:1}"
		sleep 0.08
	done
}

echo "== building"
make -C "$ROOT" build >/dev/null

# Bare `aforge chat`, with the SSH variables cleared so a local run is local
# even when the shell that started the harness arrived over SSH itself.
RUN="cd $WS && set -a && . $ENVFILE && set +a && env -u SSH_CONNECTION -u SSH_TTY -u SSH_CLIENT $ROOT/bin/aforge chat"
if [[ "$MODE" == ssh ]]; then
	RUN="ssh -t -o BatchMode=yes localhost 'cd $WS && set -a && . $ENVFILE && set +a && $ROOT/bin/aforge chat'"
fi

echo "== $MODE: driving a scripted minute in tmux ($SESSION, 130x40)"
tmux new-session -d -s "$SESSION" -x 130 -y 40 -c "$WS"
tmux send-keys -t "$SESSION" "$RUN" Enter

# boot: exec through the first full paint and the splash that animates over it.
mark boot
sleep 8

# idle: the prompt is up and nobody is touching it. Whatever this costs is what
# a surface costs a person who left it open in another window.
mark idle
sleep 12

# typing: one keystroke at a time. Every one of these is a round trip on a real
# link, so what matters here is bytes per keystroke, not bytes per second.
mark typing
type_slowly "Write 300 words about the history of terminal emulators."

# waiting: the request is away and the first token has not landed. The surface
# is animating and saying nothing.
mark waiting
tmux send-keys -t "$SESSION" Enter
sleep 5

# streaming: the reply arriving, a token at a time, into a transcript that
# reflows as it grows.
mark streaming
sleep 40

# rest: the answer is complete and on screen, and nothing is happening again.
mark rest
sleep 20

mark done
tmux capture-pane -t "$SESSION" -p >"$OUT/pane-$MODE.txt"
tmux send-keys -t "$SESSION" -l "/quit"
tmux send-keys -t "$SESSION" Enter
sleep 2
cleanup

echo
python3 "$ROOT/perf-report/ssh/summarize.py" "$WIRE" "$PHASES"
echo
echo "log:    $WIRE"
echo "phases: $PHASES"
echo "pane:   $OUT/pane-$MODE.txt"

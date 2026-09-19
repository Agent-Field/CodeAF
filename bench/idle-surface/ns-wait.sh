#!/usr/bin/env bash
# ns-wait.sh - one measured point in a WAITING-WITH-WORK-IN-FLIGHT state, run
# INSIDE `unshare -rn --mount` by wait.sh. Args:
#   RUN BIN STUB PORT K MODE WIN RESULTS CLK_TCK
# MODE=nodelta: after K normal turns the delay file is set to 90000ms and one
#   more message is sent; both sample windows sit in the request-sent,
#   no-first-delta state (spinner only, paint clock running, no streaming).
# MODE=stream: the delay file is set to 1500ms; 40 tokens take ~60s, so
#   window1 samples mid-stream and window2 crosses the stream's end.
# Everything else matches ns.sh (the idle-state harness). Only processes this
# tree started are killed, by pid, after reading each cmdline.
set -eu
RUN="$1"; BIN="$2"; STUB="$3"; PORT="$4"; K="$5"; MODE="$6"; WIN="$7"; RESULTS="$8"; CLK_TCK="$9"
DELAY_FILE="$RUN/delay"

log() { printf '%s %s\n' "$(date +%T)" "$*" >&2; }

ip link set lo up
mount -t tmpfs tmpfs /tmp

case "$MODE" in
  nodelta) HOLD_MS=90000 ;;
  stream)  HOLD_MS=1500 ;;
  *) echo "bad mode $MODE" >&2; exit 2 ;;
esac

STUB_LOG="$RUN/stub.log" STUB_PORT="$PORT" STUB_TOKENS=40 STUB_DELAY_MS=5 \
  STUB_DELAY_FILE="$DELAY_FILE" \
  python3 "$STUB" > "$RUN/stub.out" 2>&1 &
SPID=$!
echo 5 > "$DELAY_FILE"
for _ in $(seq 1 50); do
  curl -sf "http://127.0.0.1:$PORT/v1/models" -o /dev/null 2>/dev/null && break
  sleep 0.2
done
log "stub up pid=$SPID mode=$MODE hold_ms=$HOLD_MS"

mkdir -p "$RUN/tmux-home"
export HOME="$RUN/prof"
export TMUX_TMPDIR="$RUN/tmux-home"
export CODEAF_BASE_URL="http://127.0.0.1:$PORT/v1"
export OPENROUTER_API_KEY="stub-key-not-a-credential"
export CODEAF_TELEMETRY=off DO_NOT_TRACK=1 TERM=xterm-256color

SOCK="$RUN/tmux-home/idle"
tmux -S "$SOCK" new-session -d -s idle -x 140 -y 45 -c "$RUN/wd" \
  "printf '%s' \$\$ > '$RUN/surface.pid'; exec '$BIN' chat --yolo --no-host --max-cost 5" \
  > "$RUN/tmux.out" 2>&1
for _ in $(seq 1 300); do
  if tmux -S "$SOCK" capture-pane -p -t '=idle:' 2>/dev/null | grep -q 'say what you want done'; then break; fi
  tmux -S "$SOCK" has-session -t '=idle:' 2>/dev/null || { log 'surface died before composer'; exit 3; }
  sleep 0.5
done
SP=$(cat "$RUN/surface.pid")
log "surface up pid=$SP"

pane()   { tmux -S "$SOCK" capture-pane -p -t '=idle:' 2>/dev/null; }
is_idle(){ printf '%s' "$(pane)" | grep -Eq '( idle$|idle[[:space:]]*$)'; }
dones() { grep -c ' DONE ' "$RUN/stub.log" 2>/dev/null || true; }

for i in $(seq 1 "$K"); do
  buf="$RUN/msg"; printf 'turn %s: reply with the single word ok' "$i" > "$buf"
  tmux -S "$SOCK" load-buffer -b idlemsg "$buf"
  tmux -S "$SOCK" paste-buffer -d -p -b idlemsg -t '=idle:'
  sleep 1
  tmux -S "$SOCK" send-keys -t '=idle:' Enter
  deadline=$(( $(date +%s) + 240 ))
  while :; do
    [ "$(date +%s)" -ge "$deadline" ] && { log "turn $i timed out"; exit 4; }
    if [ "$(dones)" -ge "$i" ] && is_idle; then break; fi
    sleep 1
  done
done
log "turns done: $K"

# hold the final turn open
printf 'hold one more turn with the delay file at %sms\n' "$HOLD_MS" > "$RUN/msg"
echo "$HOLD_MS" > "$DELAY_FILE"
tmux -S "$SOCK" load-buffer -b idlemsg "$RUN/msg"
tmux -S "$SOCK" paste-buffer -d -p -b idlemsg -t '=idle:'
sleep 1
tmux -S "$SOCK" send-keys -t '=idle:' Enter
posts=$(grep -c ' POST ' "$RUN/stub.log" 2>/dev/null || echo 0)
if [ "$MODE" = stream ]; then
  sleep 8            # first delta at ~1.5s; sample mid-stream
else
  sleep 4            # nothing arrives for 90s; sample the pure wait
fi
log "held turn sent (posts=$posts); sampling"

stat_ticks() { local pid="$1" f t sum=0
  for f in /proc/$pid/task/[0-9]*/stat; do
    [ -r "$f" ] || continue
    t=$(awk '{print $14+$15}' "$f") || continue
    sum=$((sum + ${t:-0}))
  done; printf '%s' "$sum"; }
threads_of() { ls -d /proc/$1/task/[0-9]* 2>/dev/null | wc -l; }
vmrss()     { awk '/^VmRSS:/{print $2; exit}' /proc/$1/status 2>/dev/null; }
busyword()  { printf '%s' "$(pane)" | tail -3 | grep -oE '(working|idle|interrupted)' | tail -1; }

t0=$(stat_ticks "$SP"); sleep "$WIN"; t1=$(stat_ticks "$SP")
rss1=$(vmrss "$SP"); thr=$(threads_of "$SP"); w1state=$(busyword)
log "window1 ticks=$((t1-t0)) rss=$rss1 threads=$thr state=$w1state"
sleep "$WIN"; t2=$(stat_ticks "$SP"); rss2=$(vmrss "$SP"); thr2=$(threads_of "$SP"); w2state=$(busyword)
log "window2 ticks=$((t2-t1)) rss=$rss2 threads=$thr2 state=$w2state"
posts2=$(grep -c ' POST ' "$RUN/stub.log" 2>/dev/null || echo 0)
deltas=$(grep -c FIRST_DELTA "$RUN/stub.log" 2>/dev/null || echo 0)
pane | tail -30 > "$RUN/out/final-screen.txt"

python3 - "$RUN/point.json" <<PY
import json
json.dump({"ticks1": int("$((t1-t0))"), "ticks2": int("$((t2-t1))"),
  "threads": int("$thr"), "threads2": int("$thr2"),
  "rss_surf": int("${rss1:-0}"), "rss_surf2": int("${rss2:-0}"),
  "turns": int("$K"), "mode": "$MODE", "hold_ms": int("$HOLD_MS"),
  "posts_before_hold": int("$posts"), "posts_after": int("$posts2"),
  "first_deltas": int("$deltas"),
  "state": "$MODE hold: pane state w1=$w1state w2=$w2state"},
  open("$RUN/point.json", "w"))
PY
python3 - "$RESULTS" "$RUN/point.json" "$CLK_TCK" "$WIN" <<'PY'
import json,sys
res,pt,clk,win=sys.argv[1],json.load(open(sys.argv[2])),int(sys.argv[3]),int(sys.argv[4])
pct=lambda d: round(100.0*d/(win*clk),3)
with open(res,"a") as fh:
    fh.write("\t".join(map(str,[pt["turns"],pt["mode"],pt["threads"],
        pct(pt["ticks1"]),pct(pt["ticks2"]),
        round(pt["rss_surf"]/1024,1), round(pt["rss_surf2"]/1024,1),
        pt["posts_before_hold"], pt["posts_after"], pt["first_deltas"],
        pt["state"]]))+"\n")
print("K=%s mode=%s w1=%.3f%%core w2=%.3f%%core threads=%s rss=%s/%sKB posts=%s->%s deltas=%d" %
      (pt["turns"],pt["mode"],pct(pt["ticks1"]),pct(pt["ticks2"]),pt["threads"],
       pt["rss_surf"],pt["rss_surf2"],pt["posts_before_hold"],pt["posts_after"],pt["first_deltas"]))
PY

tmux -S "$SOCK" kill-session -t '=idle:' 2>/dev/null || true
for p in "$SP" "$SPID"; do
  if [ -e "/proc/$p/cmdline" ]; then
    { printf 'killing %s: ' "$p"; tr '\0' ' ' < "/proc/$p/cmdline"; printf '\n'; } >&2
    kill -TERM "$p" 2>/dev/null || true
  fi
done
log "point done"

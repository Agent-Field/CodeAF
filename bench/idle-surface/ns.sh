#!/usr/bin/env bash
# ns.sh - one measured point, run INSIDE `unshare -rn --mount` by replay.sh.
# Args: RUN BIN STUB PORT K WIN RESULTS CLK_TCK
# Every process here was started by this tree and is killed by pid at the end,
# after reading its proc cmdline to confirm it.
set -eu
RUN="$1"; BIN="$2"; STUB="$3"; PORT="$4"; K="$5"; WIN="$6"; RESULTS="$7"; CLK_TCK="$8"

log() { printf '%s %s\n' "$(date +%T)" "$*" >&2; }

ip link set lo up
mount -t tmpfs tmpfs /tmp          # own /tmp for this namespace

# stub: the only other process, on loopback inside this netns
STUB_LOG="$RUN/stub.log" STUB_PORT="$PORT" STUB_TOKENS=40 STUB_DELAY_MS=5 \
  python3 "$STUB" > "$RUN/stub.out" 2>&1 &
SPID=$!
for _ in $(seq 1 50); do
  curl -sf "http://127.0.0.1:$PORT/v1/models" -o /dev/null 2>/dev/null && break
  sleep 0.2
done
log "stub up pid=$SPID"

mkdir -p "$RUN/tmux-home"
export HOME="$RUN/prof"
export TMUX_TMPDIR="$RUN/tmux-home"    # private socket dir, nothing shared
export CODEAF_BASE_URL="http://127.0.0.1:$PORT/v1"
export OPENROUTER_API_KEY="stub-key-not-a-credential"
export CODEAF_TELEMETRY=off DO_NOT_TRACK=1 TERM=xterm-256color

SOCK="$RUN/tmux-home/idle"
tmux -S "$SOCK" new-session -d -s idle -x 140 -y 45 -c "$RUN/wd" \
  "printf '%s' \$\$ > '$RUN/surface.pid'; exec '$BIN' chat --yolo --no-host --max-cost 5" \
  > "$RUN/tmux.out" 2>&1
for _ in $(seq 1 300); do            # composer up = status row says idle
  if tmux -S "$SOCK" capture-pane -p -t '=idle:' 2>/dev/null | grep -q 'say what you want done'; then break; fi
  tmux -S "$SOCK" has-session -t '=idle:' 2>/dev/null || { log 'surface died before composer'; exit 3; }
  sleep 0.5
done
SP=$(cat "$RUN/surface.pid")
log "surface up pid=$SP"

pane()   { tmux -S "$SOCK" capture-pane -p -t '=idle:' 2>/dev/null; }
is_idle(){ printf '%s' "$(pane)" | grep -Eq '( idle$|idle[[:space:]]*$)'; }
is_busy(){ printf '%s' "$(pane)" | grep -Eq '( (working|interrupted)$|[0-9]+ running)'; }

dones() { grep -c ' DONE ' "$RUN/stub.log" 2>/dev/null || true; }
turns=0
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
  turns=$i
done
log "turns done: $turns"

# WAITING STATE: nothing more is sent. The K-th answer finished and the status
# row reads idle; it is held through both sample windows.

stat_ticks() { local pid="$1" f t sum=0
  for f in /proc/$pid/task/[0-9]*/stat; do
    [ -r "$f" ] || continue
    t=$(awk '{print $14+$15}' "$f") || continue
    sum=$((sum + ${t:-0}))
  done; printf '%s' "$sum"; }
threads_of() { ls -d /proc/$1/task/[0-9]* 2>/dev/null | wc -l; }
vmrss()     { awk '/^VmRSS:/{print $2; exit}' /proc/$1/status 2>/dev/null; }

daemon_rss() {  # first direct child of the surface whose cmdline is our binary
  local ch f
  for f in /proc/$1/task/[0-9]*/children; do
    [ -r "$f" ] || { continue; }
    while read -r ch; do
      [ -n "$ch" ] || continue
      if tr '\0' ' ' < "/proc/$ch/cmdline" 2>/dev/null | grep -q codeaf; then
        printf '%s' "$(vmrss "$ch")"; return 0; fi
    done < "$f"
  done
  printf ''; }

sleep 3
t0=$(stat_ticks "$SP"); sleep "$WIN"; t1=$(stat_ticks "$SP")
rss1=$(vmrss "$SP"); thr=$(threads_of "$SP"); dae1=$(daemon_rss "$SP")
log "window1 ticks=$((t1-t0)) rss=$rss1 threads=$thr daemon=${dae1:-none}"
sleep "$WIN"; t2=$(stat_ticks "$SP"); rss2=$(vmrss "$SP"); thr2=$(threads_of "$SP"); dae2=$(daemon_rss "$SP")
log "window2 ticks=$((t2-t1)) rss=$rss2 threads=$thr2 daemon=${dae2:-none}"

kb=$(du -sk "$RUN/prof/.codeaf" 2>/dev/null | cut -f1)
pane | tail -30 > "$RUN/out/final-screen.txt"

python3 - "$RUN/point.json" <<PY
import json
json.dump({"ticks1": int("$((t1-t0))"), "ticks2": int("$((t2-t1))"),
  "threads": int("$thr"), "rss_surf": int("$rss1"),
  "rss_daemon": (int("$dae1") if "$dae1" else None),
  "transcript_kb": int("$kb"), "turns": int("$turns"),
  "state": "idle after last completed turn, nothing in flight, nothing more sent"},
  open("$RUN/point.json", "w"))
PY
python3 - "$RESULTS" "$RUN/point.json" "$CLK_TCK" "$WIN" <<'PY'
import json,sys
res,pt,clk,win=sys.argv[1],json.load(open(sys.argv[2])),int(sys.argv[3]),int(sys.argv[4])
pct=lambda d: round(100.0*d/(win*clk),3)
dae = pt["rss_daemon"]
with open(res,"a") as fh:
    fh.write("\t".join(map(str,[pt["turns"],pt["threads"],pct(pt["ticks1"]),pct(pt["ticks2"]),
        round(pt["rss_surf"]/1024,1), (round(dae/1024,1) if dae is not None else "-"),
        pt["turns"], pt["transcript_kb"], pt["state"]]))+"\n")
print("K=%s w1=%.3f%%core w2=%.3f%%core threads=%s rss_surf=%sKB daemon=%s" %
      (pt["turns"],pct(pt["ticks1"]),pct(pt["ticks2"]),pt["threads"],pt["rss_surf"],dae))
PY

# teardown: only pids this run started, each confirmed by cmdline first
tmux -S "$SOCK" kill-session -t '=idle:' 2>/dev/null || true
for p in "$SP" "$SPID"; do
  if [ -e "/proc/$p/cmdline" ]; then
    { printf 'killing %s: ' "$p"; tr '\0' ' ' < "/proc/$p/cmdline"; printf '\n'; } >&2
    kill -TERM "$p" 2>/dev/null || true
  fi
done
log "point done"

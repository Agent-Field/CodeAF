#!/bin/bash
# scale2.sh <name> <N> <command...>
#
# N idle instances of one CLI, each in its own pane on a PRIVATE tmux socket.
#
# WHY THIS REPLACES scale.sh: scale.sh gated on "the pane has painted", which any
# non-blank pane satisfies instantly, so at N=16 it sampled CodeAF while five of
# sixteen surfaces were still starting. The two populations were plain in the
# output: eleven surfaces at about 44 MB PSS and five at about 20 MB. A figure
# taken then is a half-warmed one, not a steady state.
#
# THE GATE IS CONVERGENCE, NOT PAINT. The run waits until the tracked tree's total
# RSS stops moving: CONV_STABLE consecutive samples whose change is under
# CONV_PCT percent. It records how long that took and whether it timed out, so a
# CLI that never settles is visible instead of silently averaged. This is
# CLI-agnostic, which "has it painted" is not.
#
# The walk is c262's: /proc children files carry no trailing newline, so `read`
# fills the array and THEN reports EOF. The populated array is truth.
#
# Teardown kills ONLY pids this script discovered, by pid, each confirmed from its
# own /proc/<pid>/cmdline. No pattern kill anywhere, no HOME sweep.
set -u

NAME="${1:?name}"; N="${2:?N}"; shift 2
[ "$#" -ge 1 ] || { echo "need a command" >&2; exit 2; }

HOMEDIR="${HOMEDIR:-$HOME}"
WORKDIR="${WORKDIR:-$PWD}"
WORKSPACES="${WORKSPACES:-same}"        # same | list
WS_LIST="${WS_LIST:-}"                  # colon-separated real checkouts
IDLE_SECONDS="${IDLE_SECONDS:-20}"
CONV_PCT="${CONV_PCT:-1}"               # percent change counted as stable
CONV_STABLE="${CONV_STABLE:-3}"         # consecutive stable samples required
CONV_TIMEOUT="${CONV_TIMEOUT:-180}"     # seconds before giving up on settling
CONV_EVERY="${CONV_EVERY:-2}"
PSI_BAR="${PSI_BAR:-1.0}"
MEMAVAIL_FLOOR="${MEMAVAIL_FLOOR:-8388608}"
COLS=160; ROWS=48

SOCK="scale2-${NAME}-$$"
TM=(tmux -L "$SOCK")
PHASE=meta
kv() { printf 'name=%s n=%s ws=%s phase=%s %s\n' "$NAME" "$N" "$WORKSPACES" "$PHASE" "$*"; }
psi10() { awk '/^some/{for(i=1;i<=NF;i++) if($i ~ /^avg10=/){sub("avg10=","",$i); print $i}}' /proc/pressure/memory 2>/dev/null || echo 0; }
memavail() { awk '/^MemAvailable:/{print $2}' /proc/meminfo; }
over() { awk -v a="$1" -v b="$2" 'BEGIN{exit !(a>b)}'; }

declare -A TRACKED=()
declare -a PANES=() KIDS_OUT=()

children_of() {
  local p="$1" t; local -a kids=()
  KIDS_OUT=()
  for t in /proc/"$p"/task/*; do
    [ -r "$t/children" ] || continue
    kids=()
    read -r -a kids < "$t/children" 2>/dev/null || [ "${#kids[@]}" -gt 0 ] || continue
    KIDS_OUT+=("${kids[@]}")
  done
}

discover() {
  local root p c; local -a frontier=() next=()
  for root in ${PANES+"${PANES[@]}"}; do
    [ -d "/proc/$root" ] || continue
    frontier=("$root"); TRACKED[$root]=1
    while [ "${#frontier[@]}" -gt 0 ]; do
      next=()
      for p in "${frontier[@]}"; do
        children_of "$p"
        for c in ${KIDS_OUT+"${KIDS_OUT[@]}"}; do
          [ -n "${TRACKED[$c]:-}" ] && continue
          TRACKED[$c]=1; next+=("$c")
        done
      done
      frontier=(${next+"${next[@]}"})
    done
  done
}

tree_rss() { # total RSS over tracked pids, in kB
  local p t=0 v
  for p in "${!TRACKED[@]}"; do
    [ -r "/proc/$p/statm" ] || continue
    read -r _ v _ < "/proc/$p/statm" 2>/dev/null || continue
    t=$(( t + v * 4 ))
  done
  printf '%s' "$t"
}

kv "host=$(hostname) cores=$(nproc) workdir=$WORKDIR ws_list=${WS_LIST:-none}"
kv "convergence: <${CONV_PCT}% change over ${CONV_STABLE} samples ${CONV_EVERY}s apart, timeout ${CONV_TIMEOUT}s"
P0=$(psi10); M0=$(memavail)
kv "gate_before pressure_some_avg10=$P0 memavailable_kb=$M0 load1=$(cut -d' ' -f1 /proc/loadavg)"
over "$P0" "$PSI_BAR" && { kv "SKIPPED pressure $P0 over $PSI_BAR"; exit 0; }
[ "$M0" -ge "$MEMAVAIL_FLOOR" ] || { kv "SKIPPED memavailable $M0 under floor"; exit 0; }

EXE=$(readlink -f "$1" 2>/dev/null || command -v "$1")
prior=0; priorpids=""
for p in $(ls /proc 2>/dev/null | grep -E '^[0-9]+$'); do
  e=$(readlink -f "/proc/$p/exe" 2>/dev/null) || continue
  [ "$e" = "$EXE" ] && { prior=$((prior+1)); priorpids="$priorpids $p"; }
done
kv "exe=$EXE prior_own_processes=$prior prior_pids='${priorpids# }'"

PHASE=launch
i=0
while [ "$i" -lt "$N" ]; do
  i=$((i+1))
  wd="$WORKDIR"
  if [ "$WORKSPACES" = list ]; then
    wd=$(printf '%s' "$WS_LIST" | cut -d: -f"$i")
    if [ -z "$wd" ] || [ ! -d "$wd" ]; then kv "FAILED workspace $i absent from WS_LIST"; break; fi
  fi
  "${TM[@]}" new-session -d -s "s$i" -x "$COLS" -y "$ROWS" -c "$wd" \
    env HOME="$HOMEDIR" TERM=xterm-256color "$@" 2>/dev/null \
    || { kv "FAILED tmux new-session $i"; break; }
done
sleep 1
mapfile -t PANES < <("${TM[@]}" list-panes -a -F '#{pane_pid}' 2>/dev/null)
kv "panes=${#PANES[@]}"
[ "${#PANES[@]}" -gt 0 ] || { kv "no panes"; "${TM[@]}" kill-server 2>/dev/null; exit 1; }

PHASE=converge
last=0; stable=0; waited=0; conv=timeout
while [ "$waited" -lt "$CONV_TIMEOUT" ]; do
  sleep "$CONV_EVERY"; waited=$(( waited + CONV_EVERY ))
  discover
  now=$(tree_rss)
  if [ "$last" -gt 0 ]; then
    if awk -v a="$last" -v b="$now" -v p="$CONV_PCT" \
        'BEGIN{d=b-a; if(d<0)d=-d; exit !(a>0 && 100*d/a < p)}'; then
      stable=$(( stable + 1 ))
    else
      stable=0
    fi
  fi
  last=$now
  kv "sample t=${waited}s tree_rss_kb=$now stable=$stable procs=${#TRACKED[@]}"
  [ "$stable" -ge "$CONV_STABLE" ] && { conv=settled; break; }
done
kv "converged=$conv after=${waited}s tree_rss_kb=$last"

PHASE=idle
i=0; ticks=$(( IDLE_SECONDS * 2 ))
while [ "$i" -lt "$ticks" ]; do sleep 0.5; discover; i=$((i+1)); done

PHASE=proc
TOT_RSS=0; TOT_PSS=0; TOT_HWM=0; TOT_THR=0; TOT_FDS=0; ALIVE=0; DAEMONS=""
for p in "${!TRACKED[@]}"; do
  [ -d "/proc/$p" ] || continue
  cmd=$(tr '\0' ' ' < /proc/"$p"/cmdline 2>/dev/null)
  case "$cmd" in *"$EXE"*|*"$NAME"*) ;; *) continue ;; esac
  rss=0; pss=0; hwm=0; thr=0; comm=""
  while IFS= read -r line; do
    case "$line" in
      VmRSS:*)   v=${line#*:}; read -r v _ <<<"$v"; rss=$v ;;
      VmHWM:*)   v=${line#*:}; read -r v _ <<<"$v"; hwm=$v ;;
      Threads:*) v=${line#*:}; read -r v _ <<<"$v"; thr=$v ;;
      Name:*)    v=${line#*:}; read -r v _ <<<"$v"; comm=$v ;;
    esac
  done < "/proc/$p/status" 2>/dev/null || continue
  if [ -r "/proc/$p/smaps_rollup" ]; then
    while IFS= read -r line; do
      case "$line" in Pss:*) v=${line#*:}; read -r v _ <<<"$v"; pss=$v; break ;; esac
    done < "/proc/$p/smaps_rollup" 2>/dev/null
  fi
  fds=$(ls /proc/"$p"/fd 2>/dev/null | wc -l)
  printf 'name=%s n=%s ws=%s phase=proc pid=%s comm=%s rss_kb=%s pss_kb=%s peak_rss_kb=%s threads=%s fds=%s cmd=%s\n' \
    "$NAME" "$N" "$WORKSPACES" "$p" "$comm" "$rss" "$pss" "$hwm" "$thr" "$fds" "$cmd"
  TOT_RSS=$((TOT_RSS+rss)); TOT_PSS=$((TOT_PSS+pss)); TOT_HWM=$((TOT_HWM+hwm))
  TOT_THR=$((TOT_THR+thr)); TOT_FDS=$((TOT_FDS+fds)); ALIVE=$((ALIVE+1))
  case "$cmd" in *--daemon*) DAEMONS="$DAEMONS $p" ;; esac
done

PHASE=summary
kv "procs=$ALIVE tracked=${#TRACKED[@]} daemons=$(printf '%s' "$DAEMONS" | wc -w) converged=$conv converged_after_s=$waited"
kv "rss_kb=$TOT_RSS pss_kb=$TOT_PSS peak_rss_kb=$TOT_HWM threads=$TOT_THR fds=$TOT_FDS"
kv "gate_after pressure_some_avg10=$(psi10) memavailable_kb=$(memavail) load1=$(cut -d' ' -f1 /proc/loadavg)"

PHASE=teardown
killed=0
for p in "${!TRACKED[@]}"; do
  [ -d "/proc/$p" ] || continue
  c=$(tr '\0' ' ' < /proc/"$p"/cmdline 2>/dev/null); [ -n "$c" ] || continue
  kill -TERM "$p" 2>/dev/null && killed=$((killed+1))
done
sleep 3
for p in "${!TRACKED[@]}"; do [ -d "/proc/$p" ] && kill -KILL "$p" 2>/dev/null; done
"${TM[@]}" kill-server 2>/dev/null
sleep 1
left=0; for p in "${!TRACKED[@]}"; do [ -d "/proc/$p" ] && left=$((left+1)); done
kv "signalled=$killed owned_survivors=$left"
rm -f "/tmp/tmux-$(id -u)/$SOCK" 2>/dev/null

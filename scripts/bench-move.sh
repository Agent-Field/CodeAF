#!/usr/bin/env bash
# bench-move.sh: one command reproduces the move-a-chat benchmark matrix and writes JSON.
#
#   scripts/bench-move.sh [--envs same,second] [--sizes S,M,L] [--reps 3] [--out DIR]
#                         [--kinds one,fifty,big] [--big-mb 100] [--latency-n 20]
#                         [--keep] [--skip-build]
#
# What it measures (docs/testing-anywhere.md says how to read it):
#   cold take  a device that never had the chat          warm take  a device holding an older head
#   take-back  to the first machine                     publish    seal to durable, per change size
#   phases     seal / upload / directory / fetch / materialize, bytes, requests, and dollars.
# No model is called: sealing is driven by scripted edits (scripts/measure/benchmove; main.go.txt is kept as .txt so the module does not build it).
#
# Environments:   same   both devices on this box, one relay (own docker container codeaf-bench-relay on 127.0.0.1:18788; the shared codeaf-s1-relay was recreated by another lane mid-run)
#                 second A = this box, B = the second machine (Apple Silicon macOS), B reaches the relay through ssh -R
#                 hosted A = this box, B = the second machine, both talk to the hosted relay in CODEAF_RELAY directly
# Settings (docs/testing-anywhere.md): CODEAF_SECOND_HOST and CODEAF_SECOND_ROOT (the second machine and a folder on it for
# bin/, run/ and the engine source), CODEAF_RELAY (for `hosted`), CODEAF_CORPUS (a folder holding the three repos below).
# Repo sizes come from the corpus: S r18-pareto-c365, M r02-mj-base, L r06-agentfield.
# The product is built from CODEAF_SRC (a checkout; default the benchmove worktree) and its sha is in every row.
# Outputs: <out>/{rows.jsonl,env.json,summary.json,summary.md}. Nothing here deploys anything.
set -euo pipefail

# The repo root is the product checkout under test unless CODEAF_SRC names another.
PROTO=$(cd "$(dirname "$0")/.." && pwd)
SRC=${CODEAF_SRC:-$PROTO}
ENVS=same,second SIZES=S,M,L REPS=3 KINDS=one,fifty,big BIGMB=100 LATN=20 KEEP= SKIPBUILD= OUT=
while [ $# -gt 0 ]; do
  case $1 in
    --envs) ENVS=$2; shift 2;; --sizes) SIZES=$2; shift 2;; --reps) REPS=$2; shift 2;; --out) OUT=$2; shift 2;;
    --kinds) KINDS=$2; shift 2;; --big-mb) BIGMB=$2; shift 2;; --latency-n) LATN=$2; shift 2;;
    --keep) KEEP=--keep; shift;; --skip-build) SKIPBUILD=1; shift;;
    -h|--help) sed -n '2,22p' "$0"; exit 0;; *) echo "unknown flag $1" >&2; exit 2;;
  esac
done
log() { printf '%s %s\n' "$(date +%T)" "$*" >&2; }

SHA=$(git -C "$SRC" rev-parse --short=9 HEAD)
DIRTY=false; [ -n "$(git -C "$SRC" status --porcelain --untracked-files=no)" ] && DIRTY=true
BUILD=${BENCH_BUILD:-$HOME/bench-move-build}/$SHA
OUT=${OUT:-$PROTO/.lane/bench-move/$SHA-$(date -u +%Y%m%dT%H%M%SZ)}
RELAY_NAME=${RELAY_NAME:-codeaf-bench-relay} RELAY_VOLUME=${RELAY_VOLUME:-codeaf-bench-relay-store} RELAY_PORT=${RELAY_PORT:-18788}
export RELAY_NAME
mkdir -p "$OUT" "$BUILD"
log "product sha $SHA dirty=$DIRTY; out $OUT"

# ---- build: product, probe (linux, and darwin for the second machine), relay image -------------
if [ -z "$SKIPBUILD" ] || [ ! -x "$BUILD/benchmove-linux" ]; then
  log "make build in $SRC"; make -C "$SRC" build >&2
  rsync -a --delete --exclude .git --exclude engine/target "$SRC"/ "$BUILD/src"/
  mkdir -p "$BUILD/src/cmd/benchmove"; cp "$PROTO/scripts/measure/benchmove/main.go.txt" "$BUILD/src/cmd/benchmove/main.go"
  (cd "$BUILD/src" && go vet ./cmd/benchmove && go build -o "$BUILD/benchmove-linux" ./cmd/benchmove &&
    GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o "$BUILD/benchmove-darwin" ./cmd/benchmove)
  cp "$SRC/engine/target/release/furrow" "$BUILD/furrow-linux"
  cp "$SRC/bin/codeaf" "$BUILD/codeaf-linux"
fi

# ---- relay: the product binary in a docker container, image tagged by sha -----
IMG=codeaf-bench-relay:$SHA
if [ "$(docker inspect -f '{{.Config.Image}}' $RELAY_NAME 2>/dev/null)" != "$IMG" ] || ! curl -fs "http://127.0.0.1:$RELAY_PORT/status" >/dev/null; then
  log "relay: building $IMG"
  ctx=$BUILD/relay-image; mkdir -p "$ctx"; cp "$BUILD/codeaf-linux" "$ctx/codeaf"
  docker build -q -t "$IMG" -f - "$ctx" >/dev/null <<'DOCKER'
FROM ubuntu:24.04
COPY codeaf /usr/local/bin/codeaf
RUN useradd -u 10001 relay && mkdir /store && chown relay /store
USER relay
ENV CODEAF_HOME=/tmp/codeaf
VOLUME /store
EXPOSE 8787
ENTRYPOINT ["codeaf", "relay", "--listen", ":8787", "--store", "/store"]
DOCKER
  docker rm -f $RELAY_NAME >/dev/null 2>&1 || true
  docker run -d --name $RELAY_NAME --label codeaf-bench=relay -p "127.0.0.1:$RELAY_PORT:8787" -v "$RELAY_VOLUME:/store" "$IMG" >/dev/null
  for _ in $(seq 50); do curl -fs "http://127.0.0.1:$RELAY_PORT/status" >/dev/null && break; sleep 0.2; done
fi
curl -fs "http://127.0.0.1:$RELAY_PORT/status" >/dev/null || { echo "relay does not answer on $RELAY_PORT" >&2; exit 1; }

# ---- second machine: engine built there from the same tree, probe copied, tunnel to the relay ----
# Why a function: every environment but `same` needs the second machine, and the ones that need the tunnel are fewer.
uses() { [[ ",$ENVS," == *,$1,* ]]; }
needs_second() { uses second || uses hosted || uses cf-l; }
TUNNEL_PID=
cleanup() { [ -n "$TUNNEL_PID" ] && tr '\0' ' ' < /proc/$TUNNEL_PID/cmdline 2>/dev/null | grep -q -- "-R $RELAY_PORT:" && kill "$TUNNEL_PID"; true; }
trap cleanup EXIT
if needs_second; then
  SECOND=${CODEAF_SECOND_HOST:?CODEAF_SECOND_HOST is not set: it is the second machine, reachable with ssh and no password prompt}
  SECOND_ROOT=${CODEAF_SECOND_ROOT:?CODEAF_SECOND_ROOT is not set: it is an absolute folder on the second machine for the bench programs and runs}
  ssh -o BatchMode=yes "$SECOND" "mkdir -p $SECOND_ROOT/engine-src $SECOND_ROOT/bin $SECOND_ROOT/run"
  if [ -z "$SKIPBUILD" ]; then
    log "second machine: building the engine"
    rsync -a --delete --exclude target -e "ssh -o BatchMode=yes" "$SRC/engine/" "$SECOND:$SECOND_ROOT/engine-src/"
    ssh -o BatchMode=yes "$SECOND" "export PATH=/opt/homebrew/bin:\$HOME/.cargo/bin:\$PATH; cd $SECOND_ROOT/engine-src && cargo build --release --locked -p furrow-cli >/dev/null 2>&1 && cp target/release/furrow ../bin/furrow"
    scp -q -o BatchMode=yes "$BUILD/benchmove-darwin" "$SECOND:$SECOND_ROOT/bin/benchmove"
  fi
  if uses second; then   # the docker relay is reached through ssh -R; a hosted relay needs no tunnel
    if ssh -o BatchMode=yes "$SECOND" "nc -z 127.0.0.1 $RELAY_PORT"; then echo "port $RELAY_PORT is busy on $SECOND" >&2; exit 1; fi
    setsid ssh -N -o BatchMode=yes -o ExitOnForwardFailure=yes -o ServerAliveInterval=15 -R "$RELAY_PORT:127.0.0.1:$RELAY_PORT" "$SECOND" > "$OUT/tunnel.log" 2>&1 < /dev/null &
    TUNNEL_PID=$!
    for _ in $(seq 40); do ssh -o BatchMode=yes "$SECOND" "curl -fs http://127.0.0.1:$RELAY_PORT/status >/dev/null && echo ok" | grep -q ok && break; sleep 0.5; done
  fi
fi

# ---- environment record ------------------------------------------------------------
python3 - "$OUT/env.json" "$SHA" "$DIRTY" "${SECOND:-}" "$RELAY_PORT" "$ENVS" <<'PY'
import json, os, subprocess, sys, statistics, time
out, sha, dirty, second, port, envs = sys.argv[1:]
sh = lambda c: subprocess.run(c, shell=True, capture_output=True, text=True).stdout.strip()
env = {"sha": sha, "dirty": dirty == "true", "when": time.strftime("%FT%TZ", time.gmtime()),
       "a": {"uname": sh("uname -srm"), "cpus": sh("nproc"), "go": sh("go version")},
       "relay": sh("docker inspect -f '{{.Config.Image}}' " + os.environ["RELAY_NAME"])}
def rtt(cmd, n=20):
    ts = []
    for _ in range(n):
        t = time.time(); subprocess.run(cmd, shell=True, capture_output=True); ts.append((time.time() - t) * 1000)
    return round(statistics.median(ts), 2)
env["relay_status_rtt_ms_local"] = rtt(f"curl -s http://127.0.0.1:{port}/status")
if second:
    env["b"] = {"uname": sh(f"ssh -o BatchMode=yes {second} 'uname -srm'"), "cpus": sh(f"ssh -o BatchMode=yes {second} 'sysctl -n hw.ncpu'"),
                      "go": sh(f"ssh -o BatchMode=yes {second} 'export PATH=/opt/homebrew/bin:$PATH; go version'")}
if "second" in envs.split(","):
    one = f"ssh -o BatchMode=yes {second} 'for i in 1 2 3 4 5 6 7 8 9 10; do curl -s -o /dev/null -w \"%{{time_total}}\\n\" http://127.0.0.1:{port}/status; done'"
    v = [float(x) * 1000 for x in sh(one).split()]
    env["relay_status_rtt_ms_b_via_tunnel_incl_ssh"] = round(statistics.median(v), 2) if v else None
json.dump(env, open(out, "w"), indent=1)
PY

# ---- the matrix ----------------------------------------------------------------------
REAL_ENVS=$(tr ',' '\n' <<<"$ENVS" | grep -v '^$' | paste -sd, -)
log "matrix: envs=$REAL_ENVS sizes=$SIZES reps=$REPS kinds=$KINDS"
python3 "$PROTO/scripts/measure/benchmove/run.py" --out "$OUT" --sha "$SHA$([ "$DIRTY" = true ] && echo +dirty)" --url "http://127.0.0.1:$RELAY_PORT" --envs "$REAL_ENVS" \
  --sizes "$SIZES" --reps "$REPS" --kinds "$KINDS" --big-mb "$BIGMB" --latency-n "$LATN" --bin "$BUILD" \
  ${SECOND:+--second-host "$SECOND" --second-root "$SECOND_ROOT"} $KEEP
python3 "$PROTO/scripts/measure/benchmove/summarize.py" "$OUT"
log "done: $OUT/summary.json and $OUT/summary.md"

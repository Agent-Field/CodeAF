#!/usr/bin/env bash
# marathon/preflight.sh — prove the cell's environment before spending ten hours in it.
#
# Usage: bash preflight.sh [task]            (default: rust-java-lsp)
#
# Two things about a cell are invisible from inside a run and fatal to its row:
# WHICH COMPILER the agent is holding, and WHAT THE NETWORK REACHES. Both were
# wrong for seeds s4–s9 and neither showed up until the verifier scored 0.0 ten
# hours later. This script builds a throwaway container exactly as cell.sh builds
# the real one, asserts both, and prints PASS/FAIL for each.
#
# It is deliberately not a unit test of the scripts: it exercises the same
# netlock.sh and the same environment strings against the same image, because the
# only interesting failures are the ones that live in the gap between what a
# script says and what docker and the kernel do.
set -uo pipefail

MAR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TASK="${1:-rust-java-lsp}"
IMAGE="${IMAGE:-swe-marathon/$TASK:v1.1}"
CONTAINER="${PREFLIGHT_CONTAINER:-oneroad-preflight-$TASK-$$}"
PASS=0; FAIL=0

ok()   { printf '  PASS  %s\n' "$*"; PASS=$((PASS + 1)); }
bad()  { printf '  FAIL  %s\n' "$*"; FAIL=$((FAIL + 1)); }
note() { printf '        %s\n' "$*"; }
head_() { printf '\n== %s ==\n' "$*"; }

cleanup() { docker rm -f "$CONTAINER" >/dev/null 2>&1; }
trap cleanup EXIT INT TERM

docker image inspect "$IMAGE" >/dev/null 2>&1 || { echo "no image $IMAGE"; exit 2; }

head_ "the image's own toolchain"
IMAGE_RUSTC="$(docker run --rm --entrypoint sh "$IMAGE" -c 'rustc --version' 2>/dev/null)"
IMAGE_TOOLCHAIN="$(docker run --rm --entrypoint sh "$IMAGE" -c 'rustup show active-toolchain' 2>/dev/null | awk 'NR==1{print $1}')"
note "rustc     : ${IMAGE_RUSTC:-<none>}"
note "toolchain : ${IMAGE_TOOLCHAIN:-<none>}"
[ -n "$IMAGE_RUSTC" ] && [ -n "$IMAGE_TOOLCHAIN" ] \
  && ok "the image declares a toolchain" || { bad "the image declares no toolchain"; exit 1; }

# The cell's own strings, copied from cell.sh rather than reinvented here.
TOOLCHAIN_PATH="/root/.cargo/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
AGENT_ENV=(-e HOME=/chome -e RUSTUP_HOME=/root/.rustup -e CARGO_HOME=/root/.cargo
           -e "PATH=$TOOLCHAIN_PATH" -e "RUSTUP_TOOLCHAIN=$IMAGE_TOOLCHAIN")

head_ "a throwaway cell"
docker rm -f "$CONTAINER" >/dev/null 2>&1
docker run -d --name "$CONTAINER" --entrypoint sleep "$IMAGE" 1800 >/dev/null \
  && ok "container up ($CONTAINER)" || { bad "container would not start"; exit 1; }
docker exec "$CONTAINER" mkdir -p /chome

head_ "1. the defect, reproduced"
# If this ever starts PASSING as a working rustc, the image changed and the rest
# of this script is measuring nothing.
OUT="$(docker exec -e HOME=/chome "$CONTAINER" sh -c 'rustc --version 2>&1' | head -1)"
case "$OUT" in
  *"could not choose a version"*|*"no default is configured"*)
    ok "HOME=/chome alone still breaks rustup — the fix is load-bearing"
    note "$OUT" ;;
  *) bad "HOME=/chome alone no longer breaks rustup: '$OUT' — re-read this script's premise" ;;
esac

head_ "2. the agent's compiler is the image's compiler"
OUT="$(docker exec "${AGENT_ENV[@]}" "$CONTAINER" sh -c 'rustc --version 2>&1' | head -1)"
[ "$OUT" = "$IMAGE_RUSTC" ] && ok "rustc under the agent env: $OUT" \
  || bad "rustc under the agent env is '$OUT', image says '$IMAGE_RUSTC'"
OUT="$(docker exec "${AGENT_ENV[@]}" "$CONTAINER" sh -c 'cargo --version 2>&1' | head -1)"
case "$OUT" in
  cargo*) ok "cargo under the agent env: $OUT" ;;
  *)      bad "cargo under the agent env: $OUT" ;;
esac
OUT="$(docker exec "${AGENT_ENV[@]}" "$CONTAINER" sh -c 'command -v cargo' | head -1)"
[ "$OUT" = "/root/.cargo/bin/cargo" ] && ok "cargo resolves to /root/.cargo/bin — where tests/test.sh looks" \
  || bad "cargo resolves to '$OUT', not /root/.cargo/bin"

head_ "3. the egress allowlist"
bash "$MAR/netlock.sh" apply "$CONTAINER" && ok "netlock installed" || { bad "netlock would not install"; }
bash "$MAR/netlock.sh" show "$CONTAINER" | sed 's/^/        /' | head -12

probe() {  # probe <url> <reachable|blocked>
  local url="$1" want="$2" t0 t1 code
  t0="$(date +%s%N)"
  code="$(docker exec "$CONTAINER" curl -sS -o /dev/null -w '%{http_code}' --max-time 20 "$url" 2>/dev/null)"
  t1="$(date +%s%N)"
  local ms=$(( (t1 - t0) / 1000000 ))
  if [ "$want" = reachable ]; then
    # Any HTTP answer proves the TCP connection was allowed; 403 from a CDN for a
    # bare path is an answer. 000 is curl failing to connect at all.
    [ -n "$code" ] && [ "$code" != "000" ] \
      && ok "$url answered $code (${ms}ms)" || bad "$url did not answer (code=$code, ${ms}ms)"
  else
    if [ "$code" = "000" ]; then
      [ "$ms" -lt 3000 ] && ok "$url refused in ${ms}ms" \
        || bad "$url was blocked but took ${ms}ms — that is a timeout, not a REJECT"
    else
      bad "$url ANSWERED $code — it should be blocked"
    fi
  fi
}

for u in https://index.crates.io/config.json https://static.crates.io/ https://crates.io/ \
         https://github.com/ https://openrouter.ai/api/v1/models; do probe "$u" reachable; done
for u in https://pypi.org/simple/ https://registry.npmjs.org/ https://www.google.com/ \
         http://archive.ubuntu.com/ https://huggingface.co/; do probe "$u" blocked; done

head_ "4. cargo can still do its job through the allowlist"
docker exec "${AGENT_ENV[@]}" "$CONTAINER" bash -c '
  set -e; rm -rf /tmp/preflight-crate && mkdir -p /tmp/preflight-crate/src
  cd /tmp/preflight-crate
  printf "[package]\nname=\"pf\"\nversion=\"0.1.0\"\nedition=\"2021\"\n[dependencies]\nserde_json=\"1\"\n" > Cargo.toml
  printf "fn main(){println!(\"{}\", serde_json::json!({\"a\":1}));}\n" > src/main.rs
  cargo build --release 2>&1 | tail -3' > /tmp/pf-build.$$ 2>&1
if grep -q 'Finished' /tmp/pf-build.$$; then ok "a crate with a registry dependency fetched and built"
else bad "cargo build failed under the allowlist"; sed 's/^/        /' /tmp/pf-build.$$; fi
rm -f /tmp/pf-build.$$

head_ "5. what the agent installs cannot change what the agent compiles with"
# static.rust-lang.org is ON the benchmark's allowlist and shares a Fastly
# address with the crate registries, so an address-based allowlist cannot refuse
# it — and refusing it would deviate from task.toml anyway. The guarantee that
# matters is therefore not "nothing can be installed" but "nothing that is
# installed can change the compiler the verifier will also use".
OUT="$(docker exec "${AGENT_ENV[@]}" "$CONTAINER" sh -c 'rustup default stable 2>&1' | tail -4)"
note "$(printf '%s' "$OUT" | tr '\n' '|')"
INSTALLED="$(docker exec "$CONTAINER" sh -c 'ls /root/.rustup/toolchains' | tr '\n' ' ')"
note "toolchains now installed: $INSTALLED"
AFTER="$(docker exec "${AGENT_ENV[@]}" "$CONTAINER" sh -c 'rustc --version 2>&1' | head -1)"
[ "$AFTER" = "$IMAGE_RUSTC" ] && ok "after 'rustup default stable', the agent's rustc is still $AFTER" \
  || bad "after 'rustup default stable', the agent's rustc became '$AFTER'"
AFTER="$(docker exec -e "RUSTUP_TOOLCHAIN=$IMAGE_TOOLCHAIN" "$CONTAINER" sh -c 'rustc --version 2>&1' | head -1)"
[ "$AFTER" = "$IMAGE_RUSTC" ] && ok "and so is the verifier's, which cell.sh hands the same pin" \
  || bad "the verifier's rustc would be '$AFTER'"

head_ "6. the lock survives, and re-locks itself"
bash "$MAR/netlock.sh" apply "$CONTAINER" >/dev/null 2>&1
docker exec "$CONTAINER" sh -c 'true'
PID="$(docker inspect -f '{{.State.Pid}}' "$CONTAINER")"
sudo -n nsenter -t "$PID" -n /usr/sbin/iptables -F OUTPUT
probe https://www.google.com/ reachable >/dev/null 2>&1   # flushed: it is open again
bash "$MAR/netlock.sh" apply "$CONTAINER" >/dev/null 2>&1
probe https://www.google.com/ blocked

printf '\n== %d passed, %d failed ==\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ] && { echo "PREFLIGHT PASS"; exit 0; } || { echo "PREFLIGHT FAIL"; exit 1; }

#!/usr/bin/env bash
# marathon/toolchain.sh — WHICH COMPILER IS THIS CELL'S, and how a second
# container adopts it.
#
# Usage: toolchain.sh detect <agent-container>
#          prints one line: "<toolchain> <rustup_home> <cargo_home>"
#        toolchain.sh adopt <scorer-container> <toolchain>
#          installs and defaults that toolchain in the scorer, prints its rustc
#
# THE RULE, AND WHY IT IS INHERITANCE RATHER THAN A PIN.
#
# task.toml's allowlist includes static.rust-lang.org, and the official agent
# runs as root with HOME=/root. So `rustup default stable` is a LEGITIMATE move
# for the agent to make — and because the official verifier runs in the SAME
# container with PATH=/root/.cargo/bin and HOME=/root, it reads the same
# /root/.rustup and INHERITS the upgrade. The compiler the agent chose is the
# compiler the agent is judged with. That is not a loophole; it is the shape of
# the task, and today it is close to unavoidable: `lsp-types` depends on `url`,
# any fresh resolution lands on url 2.5.8 -> idna -> icu 2.3, and that needs
# rustc >= 1.88 while the image ships 1.86.0. Two of six live seeds (s6, s8) are
# already there.
#
# Our runner broke the inheritance by giving the agent HOME=/chome: rustup's
# store went to /chome/.rustup where the verifier could never see it. The fix is
# to restore the inheritance (RUSTUP_HOME/CARGO_HOME on the image's own paths),
# NOT to forbid the upgrade — a pin would have made us stricter than the
# benchmark, which is wrong in the other direction and would have scored a
# legitimately-upgraded workspace 0.0.
#
# So everything that compiles this cell's workspace — the verifier, the hourly
# snapshot scorer, an out-of-band rescore — asks the agent's own container what
# its default toolchain is and adopts it.
#
# WHY /chome IS LOOKED AT FIRST. Cells launched before the fix (seeds s4-s9,
# 2026-08-25/26) put the agent's rustup store in /chome/.rustup and upgraded
# there. That store IS what those agents compiled with, so for those cells it is
# the honest answer to "what is the agent's default toolchain" — it is the
# official-equivalent state, just written to the wrong path by our runner. New
# cells have no /chome/.rustup at all and the answer comes from /root/.rustup,
# which is both the image's path and the official one.
set -uo pipefail

CMD="${1:?usage: toolchain.sh detect <container> | adopt <container> <toolchain>}"
CONTAINER="${2:?}"

detect() {
  local rh cargo tc
  for rh in /chome/.rustup /root/.rustup; do
    # A directory is not a store. It counts only if it holds a toolchain.
    docker exec "$CONTAINER" sh -c "ls -1 $rh/toolchains 2>/dev/null | grep -q ." || continue
    cargo="$(dirname "$rh")/.cargo"
    docker exec "$CONTAINER" sh -c "[ -d $cargo ]" 2>/dev/null || cargo=/root/.cargo
    tc="$(docker exec -e "RUSTUP_HOME=$rh" -e "CARGO_HOME=$cargo" "$CONTAINER" \
            sh -c '/root/.cargo/bin/rustup show active-toolchain 2>/dev/null' 2>/dev/null \
          | awk 'NR==1{print $1}')"
    # settings.toml is the fallback, not the first answer: an agent may have set
    # a directory override, and `rustup show active-toolchain` is the only thing
    # that knows about those.
    [ -n "$tc" ] || tc="$(docker exec "$CONTAINER" sh -c \
        "sed -n 's/^default_toolchain *= *\"\\(.*\\)\"/\\1/p' $rh/settings.toml 2>/dev/null" \
        2>/dev/null | head -1 | tr -d '\r')"
    [ -n "$tc" ] || continue
    printf '%s %s %s\n' "$tc" "$rh" "$cargo"
    return 0
  done
  return 1
}

adopt() {
  local tc="${3:?usage: toolchain.sh adopt <container> <toolchain>}"
  # `--profile minimal` is the image's own profile, and it is the difference
  # between a 60 MB download and a 500 MB one. An already-installed toolchain
  # makes both commands no-ops, which is the whole point of the cached volume:
  # the FIRST snapshot of a cell pays for the toolchain, and no later one does.
  timeout "${TOOLCHAIN_INSTALL_TIMEOUT:-900}" docker exec "$CONTAINER" sh -c "
    export PATH=/root/.cargo/bin:\$PATH
    rustup toolchain install --profile minimal --no-self-update '$tc' >&2 || exit 1
    rustup default '$tc' >&2 || exit 1
    rustc --version"
}

case "$CMD" in
  detect) detect ;;
  adopt)  adopt "$@" ;;
  *) echo "usage: toolchain.sh detect <container> | adopt <container> <toolchain>" >&2; exit 2 ;;
esac

#!/usr/bin/env bash
set -euo pipefail

# The override lets the boot contract run against an isolated disposable volume.
data_root="${CODEAF_DEVBOX_DATA_ROOT:-/data}"
export HOME="$data_root/home"
export CODEAF_HOME="$HOME/.codeaf"
export CODEAF_INSTALL_DIR="$CODEAF_HOME/bin"
export CODEAF_NO_MODIFY_PATH=1 CODEAF_NO_START=1
export PATH="$CODEAF_INSTALL_DIR:$PATH"
mkdir -p "$HOME"
umask 077

say() { printf 'codeaf devbox: %s\n' "$*"; }
marker="$data_root/.codeaf-devbox-joined"
if [[ ! -f "$marker" && -z "${CODEAF_DEVBOX_CODE:-}" ]]; then
  say 'waiting for a pairing code from codeaf devbox up'
  exec sleep infinity
fi
export CODEAF_INSTALL_NAME="$CODEAF_DEVBOX_BINARY"
install_codeaf() (
  # The release pin is only for the first install; refreshes follow the channel.
  unset VERSION
  if [[ ! -x "$CODEAF_INSTALL_DIR/$CODEAF_DEVBOX_BINARY" && -n "${CODEAF_DEVBOX_VERSION:-}" ]]; then
    if curl -fsSL "$CODEAF_DEVBOX_INSTALL_URL" | VERSION="$CODEAF_DEVBOX_VERSION" bash; then
      return 0
    fi
    say 'pinned installation failed; trying the newest channel build'
  fi
  curl -fsSL "$CODEAF_DEVBOX_INSTALL_URL" | bash
)
say 'installing codeaf'
if ! install_codeaf; then
  if [[ ! -x "$CODEAF_INSTALL_DIR/$CODEAF_DEVBOX_BINARY" ]]; then
    say 'installation failed and no earlier binary is available'
    sleep 60
    exit 1
  fi
  say 'installation failed; using the earlier binary'
fi

# Each release channel syncs through its own relay, and a pairing saves none when
# the creating laptop uses its own build's default. Pin the box to the laptop's
# relay the way a pairing that named one would, so a box whose installed build
# defaults elsewhere still shares the laptop's keys and devices. A relay saved
# later by another pairing is left alone.
if [[ -n "${CODEAF_DEVBOX_VIA:-}" && ! -e "$CODEAF_HOME/sync-url" ]]; then
  mkdir -p "$CODEAF_HOME"
  printf '%s\n' "$CODEAF_DEVBOX_VIA" > "$CODEAF_HOME/sync-url"
fi
if [[ ! -f "$marker" ]]; then
  if ! "$CODEAF_DEVBOX_BINARY" pair "$CODEAF_DEVBOX_CODE" --name "$CODEAF_DEVBOX_NAME" --via "$CODEAF_DEVBOX_VIA" </dev/null; then
    say 'pairing did not finish; restarting will retry the code'
    exec sleep infinity
  fi
  touch "$marker"
fi
# Only seed missing files; later admissions and revocations belong to this volume.
remote_dir="$CODEAF_HOME/v3/remote"
seed_remote() {
  local name="$1" value="$2" temporary
  if [[ -e "$remote_dir/$name" || -z "$value" ]]; then
    return
  fi
  mkdir -p "$remote_dir"
  chmod 700 "$remote_dir"
  temporary="$(mktemp "$remote_dir/.seed-XXXXXX")"
  if ! printf %s "$value" | base64 -d > "$temporary"; then
    rm -f "$temporary"
    return 1
  fi
  chmod 600 "$temporary"
  mv "$temporary" "$remote_dir/$name"
}
seed_remote device.key "${CODEAF_DEVBOX_REMOTE_KEY:-}"
seed_remote devices.json "${CODEAF_DEVBOX_REMOTE_DEVICES:-}"
unset CODEAF_DEVBOX_CODE CODEAF_DEVBOX_REMOTE_KEY CODEAF_DEVBOX_REMOTE_DEVICES
mkdir -p "$HOME/work"
say 'serving the work folder'
# A box only serves, so it asks serve to keep the identity's saved keys,
# settings and presence in step; builds that predate the variable ignore it.
export CODEAF_SERVE_SYNC=1
# A box also takes the identity's tasks, but only from a build whose serve lists
# `--take-tasks`: a box can follow a channel whose build predates the flag, and
# serve refuses a flag it does not know, which would leave the box serving
# nothing. Two exec lines rather than an array, because `set -u` rejects an
# empty array on the bash 3.2 a Mac runs this script's test with.
serve_help="$("$CODEAF_DEVBOX_BINARY" serve --help 2>&1 </dev/null || true)"
if [[ "$serve_help" == *--take-tasks* ]]; then
  exec "$CODEAF_DEVBOX_BINARY" serve --workspace "$HOME/work" --via "$CODEAF_DEVBOX_VIA" --take-tasks
fi
exec "$CODEAF_DEVBOX_BINARY" serve --workspace "$HOME/work" --via "$CODEAF_DEVBOX_VIA"

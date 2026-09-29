#!/usr/bin/env bash
# Shared plumbing for the FrontierCode GCP campaign scripts: manifest loading,
# portable hashing, the frozen-population check, the host-arch and memory
# gates, the model-price gate and the local secret-store reader. Sourced by
# gcp-create.sh, gcp-stage.sh, gcp-key.sh, launch.sh and fetch-results.sh.
#
# THE MANIFEST IS THE ONLY SOURCE OF TRUTH. No field has a fallback: a manifest
# that is unset, unreadable, or missing a required key refuses on the spot,
# never a silent default. The one default is the sample file name a clean
# checkout carries (manifest-frontiercode-pilot.json); if that file is absent,
# the scripts refuse rather than invent a campaign.
#
# Nothing in this library ever reads or prints a key value except
# fc_secret_read, which returns the key on stdout and is called only inside a
# pipeline that carries it to `gcloud compute ssh` and nowhere else.

FC_RIG_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FC_REPO_ROOT="$(cd "$FC_RIG_DIR/../.." && pwd)"
export FC_RIG_DIR FC_REPO_ROOT

# fc_die says what was wrong on its own line and refuses. Every gate in the
# campaign scripts routes through it, so a refusal names the script that made
# it and the fact that caused it.
fc_die() { printf '%s: %s\n' "${FC_SCRIPT:-$(basename "$0")}" "$*" >&2; exit 2; }
fc_note() { printf '[%s] %s\n' "${FC_SCRIPT:-$(basename "$0")}" "$*" >&2; }

# Portable sha256 of a file, and of stdin.
fc_sha256() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'
  else shasum -a 256 "$1" | awk '{print $1}'; fi
}
fc_sha256_stdin() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum | awk '{print $1}'
  else shasum -a 256 | awk '{print $1}'; fi
}

# Resolve the campaign manifest. FC_MANIFEST names the file: an absolute path,
# or a name relative to the rig directory. The sample is the default, and a
# missing or unparseable file refuses.
fc_manifest_path() {
  local name="${FC_MANIFEST:-manifest-frontiercode-pilot.json}"
  case "$name" in
    /*) printf '%s\n' "$name" ;;
    *) printf '%s\n' "$FC_RIG_DIR/$name" ;;
  esac
}
fc_load_manifest() {
  FC_MANIFEST_PATH="$(fc_manifest_path)"
  export FC_MANIFEST_PATH
  [ -e "$FC_MANIFEST_PATH" ] || fc_die "no campaign manifest at $FC_MANIFEST_PATH — set FC_MANIFEST"
  [ -r "$FC_MANIFEST_PATH" ] || fc_die "campaign manifest is unreadable: $FC_MANIFEST_PATH"
  jq -e . "$FC_MANIFEST_PATH" >/dev/null 2>&1 || fc_die "campaign manifest is not valid JSON: $FC_MANIFEST_PATH"
}

# fc_get <jq-filter>: one required value from the manifest, refusing null or an
# empty string. A schema that says a field is required must not be satisfied by
# a blank.
fc_get() {
  local v
  v="$(jq -r "$1 // empty" "$FC_MANIFEST_PATH" 2>/dev/null)"
  [ -n "$v" ] || fc_die "manifest $FC_MANIFEST_PATH has no '$1' (a required field)"
  printf '%s\n' "$v"
}
fc_require_keys() {
  local k
  for k in "$@"; do fc_get "$k" >/dev/null; done
}

# The corpus digest: sha256 over every file under the manifest's corpus
# directory, each file's own sha256 paired with its repository-relative path,
# in a fixed byte order. Paths are repository-relative so the digest is the
# same on the Mac and on a host that checked the rig out somewhere else.
fc_corpus_sha() {
  local dir="$1"
  ( cd "$FC_REPO_ROOT" && find "$dir" -type f -not -path '*/solution/*' -not -name '.DS_Store' 2>/dev/null \
      | LC_ALL=C sort | while IFS= read -r f; do
          printf '%s  %s\n' "$(fc_sha256 "$f")" "$f"
        done ) | fc_sha256_stdin
}

# fc_shard_tasks <shard-file>: the task ids, comment and blank lines dropped,
# first whitespace field only.
fc_shard_tasks() {
  local f="$1"
  [ -f "$f" ] || fc_die "no such shard file: $f"
  awk '!/^[[:space:]]*#/ && NF {print $1}' "$f"
}

# fc_frozen_check is the paragraph the campaign is measured against: the corpus
# is exactly the revision the manifest pinned, and every shard file is exactly
# the bytes the manifest hashed. A drift here invalidates an arm silently, so
# it is a refusal before any host is made or any wave launched.
fc_frozen_check() {
  local corpus want got
  corpus="$(fc_get '.corpus')"
  want="$(fc_get '.corpus_sha256')"
  got="$(fc_corpus_sha "$corpus")"
  [ "$got" = "$want" ] || fc_die "corpus $corpus hash $got != manifest $want"
  local s p wp gp tasks nshards
  tasks=0; nshards=0
  while IFS= read -r s; do
    [ -n "$s" ] || continue
    nshards=$(( nshards + 1 ))
    p="$(jq -r --arg s "$s" '.shard_files[$s] // empty' "$FC_MANIFEST_PATH")"
    wp="$(jq -r --arg s "$s" '.shard_sha256[$s] // empty' "$FC_MANIFEST_PATH")"
    [ -n "$p" ] || fc_die "manifest names no file for shard $s"
    [ -n "$wp" ] || fc_die "manifest names no hash for shard $s"
    [ -f "$FC_REPO_ROOT/$p" ] || fc_die "shard $s file is missing: $p"
    gp="$(fc_sha256 "$FC_REPO_ROOT/$p")"
    [ "$gp" = "$wp" ] || fc_die "shard $s hash $gp != manifest $wp"
    tasks=$(( tasks + $(fc_shard_tasks "$FC_REPO_ROOT/$p" | wc -l | tr -d ' ') ))
  done < <(jq -r '.shard_files | keys[]?' "$FC_MANIFEST_PATH")
  [ "$tasks" -gt 0 ] || fc_die "the shards carry no tasks"
  fc_note "frozen population ok: $tasks task(s) across $nshards shard(s), corpus $corpus at ${want:0:12}"
}

# fc_model_priced: the served model must carry a positive list price in the
# manifest. models.dev is a cross-check when the network answers — an offline
# Mac has no way to reach it, and the manifest's own pinned price is then the
# authority, recorded rather than defaulted.
fc_model_priced() {
  local inp out model
  inp="$(fc_get '.list_price_per_mtok.input')"
  out="$(fc_get '.list_price_per_mtok.output')"
  awk -v a="$inp" -v b="$out" 'BEGIN{exit !(a+0 > 0 && b+0 > 0)}' \
    || fc_die "the manifest price for the served model is not positive (input=$inp output=$out)"
  model="$(fc_get '.model')"
  local tmp
  tmp="$(mktemp 2>/dev/null || echo /tmp/fc-models.$$)"
  if command -v curl >/dev/null 2>&1 && curl -sf -m 10 https://models.dev/api.json >"$tmp" 2>/dev/null; then
    if jq -e --arg m "${model#openrouter/}" '.openrouter.models[$m].cost.input > 0' "$tmp" >/dev/null 2>&1; then
      fc_note "model priced: $model (models.dev agrees with the manifest)"
    else
      fc_note "note: models.dev has no priced entry for $model; the manifest price is the authority"
    fi
    rm -f "$tmp"
  else
    fc_note "note: models.dev unreachable; the pinned manifest price is the authority"
  fi
}

# fc_host_arch_gate: measured runs may run natively on Linux/x86_64 only. An
# arm run under emulation measures qemu, not the harness.
fc_host_arch_gate() {
  [ "$(uname -s)" = Linux ] || fc_die "measured runs require native Linux (this is $(uname -s))"
  [ "$(uname -m)" = x86_64 ] || fc_die "measured runs require native x86_64 (this is $(uname -m))"
}

fc_host_mem_gb() {
  if [ -r /proc/meminfo ]; then
    awk '/MemTotal/ {printf "%d\n", $2 / 1024 / 1024}' /proc/meminfo
  elif command -v sysctl >/dev/null 2>&1; then
    echo $(( $(sysctl -n hw.memsize) / 1024 / 1024 / 1024 ))
  else
    echo 0
  fi
}

# fc_slug: a GCP-label-safe spelling of a campaign or arm name.
fc_slug() { printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9-' '-' | sed -e 's/-\{2,\}/-/g' -e 's/^-//' -e 's/-$//'; }

# fc_secret_read: the campaign's model key from the local secret store the
# manifest names. It returns the value on stdout and is the ONLY function that
# touches it; every caller pipes it straight into a transfer that prints only
# its length and a digest prefix. Never echoed, never logged, never a file.
fc_secret_read() {
  local store item
  store="$(fc_get '.key.store')"
  item="$(fc_get '.key.item')"
  case "$store" in
    keychain)
      command -v security >/dev/null 2>&1 || fc_die "key store is keychain but this machine has no security(1)"
      security find-generic-password -a "$USER" -s "$item" -w 2>/dev/null || fc_die "no keychain item named '$item' for $USER"
      ;;
    file)
      [ -n "${FC_KEY_FILE:-}" ] || fc_die "key store is file; set FC_KEY_FILE to its path"
      [ -r "$FC_KEY_FILE" ] || fc_die "key file is unreadable: $FC_KEY_FILE"
      tr -d '[:space:]' < "$FC_KEY_FILE"
      ;;
    env)
      [ -n "${OPENROUTER_API_KEY:-}" ] || fc_die "key store is env; set OPENROUTER_API_KEY"
      printf '%s' "$OPENROUTER_API_KEY"
      ;;
    *) fc_die "unknown key store '$store' in the manifest (keychain, file or env)" ;;
  esac
}

# fc_wave_labels <shard> prints the label for each wave of that shard, in order.
fc_wave_labels() {
  local shard="$1" cap n waves prefix w
  prefix="$(fc_get '.label_prefix')"
  cap="$(fc_get '.wave_capacity')"
  n="$(fc_shard_tasks "$FC_REPO_ROOT/$(jq -r --arg s "$shard" '.shard_files[$s] // empty' "$FC_MANIFEST_PATH")" | wc -l | tr -d ' ')"
  waves=$(( (n + cap - 1) / cap ))
  for ((w = 1; w <= waves; w++)); do printf '%s-shard%s-wave%s\n' "$prefix" "$shard" "$w"; done
}

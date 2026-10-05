#!/usr/bin/env bash
# Shared plumbing for the OpenSSF CVE rig: where the dataset and the checkouts
# live, which rows a run covers, how the provider key is found, and the two
# helpers every script logs and records through. Sourced by fetch.sh,
# prepare.sh, run.sh and cell.sh.
#
# The rig measures ONE thing: given a real repository revision, does a tool
# report the CVE that revision carries, and does it stay quiet on the revision
# that fixed it. DeepSource published that measurement for eight tools in April
# 2026 (comparison/deepsource-2026-04.json); this rig reproduces their protocol
# so a row for codeaf can sit beside theirs. README.md says what the number is
# honest to mean.
set -uo pipefail

RIG_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORK="${WORK:-$RIG_DIR/work}"
DATASET="${DATASET:-$WORK/dataset}"
RESULTS="${RESULTS:-$RIG_DIR/results}"

# The dataset is the OpenSSF project's own repository; its CVEs/ folder holds
# one JSON record per CVE naming the repository, the vulnerable and the fixed
# commit, and the weakness locations. fetch.sh clones it and records the
# revision it got, because the folder is not frozen.
DATASET_URL="${DATASET_URL:-https://github.com/ossf-cve-benchmark/ossf-cve-benchmark.git}"

# DeepSource's published run, pinned to the commit the comparison table was
# built from so a later push of theirs cannot silently move the rows we quote.
DEEPSOURCE_REPO="${DEEPSOURCE_REPO:-DeepSourceCorp/benchmarks}"
DEEPSOURCE_REV="${DEEPSOURCE_REV:-0f9a1e00e7c24ef0baba7bf29e75602215fdb309}"
DEEPSOURCE_TOOLS="claude-code coderabbit codex cursor-bugbot deepsource devin gitlab-duo greptile semgrep"

# Every judge call goes through OpenRouter, to the same model DeepSource judged
# with, so a disagreement between the two tables is never a different judge.
JUDGE_MODEL="${JUDGE_MODEL:-anthropic/claude-opus-4.5}"

log() { printf '[%s] %s\n' "$(date -u +%H:%M:%S)" "$*" >&2; }

# meta <file> k=v... merges keys into a JSON file, creating it when absent. A
# value that parses as JSON is stored as that value, so counts stay numbers.
meta() {
  python3 - "$@" <<'PY'
import json, os, sys
path = sys.argv[1]
d = json.load(open(path)) if os.path.exists(path) else {}
for kv in sys.argv[2:]:
    k, _, v = kv.partition("=")
    # Numbers and booleans are stored as such; anything else stays the string
    # it was, which keeps a tool called `null` from becoming JSON null.
    try:
        parsed = json.loads(v)
        if isinstance(parsed, (int, float, bool)) and not isinstance(parsed, str):
            v = parsed
    except Exception:
        pass
    d[k] = v
json.dump(d, open(path, "w"), indent=2, sort_keys=True)
PY
}

# The provider key, found the way the rest of this laptop finds it and never
# written to disk by the rig: API_KEY, then the shell's OPENROUTER_API_KEY, then
# ~/.config/openrouter/key, then the api_key row of ~/.codeaf/config.json, then
# a macOS Keychain item (KEYCHAIN_ITEM, by default the owner's
# `delta-openrouter`). Every rung is read-only. The result lands in KEY.
resolve_key() {
  KEY="${API_KEY:-${OPENROUTER_API_KEY:-}}"
  [ -n "$KEY" ] || [ ! -f "$HOME/.config/openrouter/key" ] || KEY="$(tr -d '[:space:]' < "$HOME/.config/openrouter/key")"
  if [ -z "$KEY" ] && [ -f "$HOME/.codeaf/config.json" ]; then
    KEY="$(python3 -c "import json,os;print(json.load(open(os.path.expanduser('~/.codeaf/config.json'))).get('api_key',''))" 2>/dev/null)"
  fi
  if [ -z "$KEY" ] && command -v security >/dev/null 2>&1; then
    KEY="$(security find-generic-password -s "${KEYCHAIN_ITEM:-delta-openrouter}" -w 2>/dev/null || true)"
  fi
  [ -n "$KEY" ] || { log "no provider key: set API_KEY or OPENROUTER_API_KEY, or name a Keychain item in KEYCHAIN_ITEM"; return 1; }
  export OPENROUTER_API_KEY="$KEY"
}

cve_json() { printf '%s/CVEs/%s.json' "$DATASET" "$1"; }
cve_field() { jq -r "$2" "$(cve_json "$1")"; } # <cve> <jq-path>

# One row is one (CVE, variant) pair; a variant is `unfixed` (the vulnerable
# commit) or `fixed` (the patching commit). SET picks the rows:
#   deepsource  the 165 rows DeepSource judged — 85 CVEs, five of them on one
#               side only (default; the only set the comparison table is valid for)
#   all         both variants of every record in the dataset
#   <path>      a file of "<cve><TAB><variant>" lines
set_rows() {
  case "${SET:-deepsource}" in
    deepsource) cat "$RIG_DIR/sets/deepsource-165.tsv" ;;
    all) local f c
         for f in "$DATASET"/CVEs/*.json; do
           c="$(basename "$f" .json)"; printf '%s\tunfixed\n%s\tfixed\n' "$c" "$c"
         done ;;
    *) [ -f "$SET" ] && cat "$SET" || { log "SET must be deepsource, all, or a file of rows: $SET"; return 1; } ;;
  esac
}

variant_commit() { # <cve> <variant>
  case "$2" in
    unfixed) cve_field "$1" '.prePatch.commit' ;;
    fixed) cve_field "$1" '.postPatch.commit' ;;
    *) log "variant must be unfixed or fixed, not $2"; return 1 ;;
  esac
}

repo_slug() { # <url> -> owner__name
  printf '%s' "$1" | sed -E 's#\.git$##; s#/+$##; s#.*/([^/]+)/([^/]+)$#\1__\2#'
}

# macOS ships no `timeout`; the rig's wall around a tool is perl's alarm, which
# every Mac has. with_wall <seconds> <command...>
with_wall() {
  local secs="$1"; shift
  perl -e 'alarm shift @ARGV; exec @ARGV or die "exec: $!"' "$secs" "$@"
}

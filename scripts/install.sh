#!/usr/bin/env bash

set -euo pipefail

# The one repository releases come from. Its former name is only GitHub's
# redirect to it, so a 404 here is the answer: no other repository is asked.
REPOSITORY="Agent-Field/codeaf"
CHANNEL="${CHANNEL:-stable}"
INSTALL_NAME="${CODEAF_INSTALL_NAME:-codeaf}"
VERSION="${VERSION:-}"
VERBOSE="${VERBOSE:-0}"
NO_MODIFY_PATH="${CODEAF_NO_MODIFY_PATH:-${AFORGE_NO_MODIFY_PATH:-0}}" # legacy-name
NO_START="${CODEAF_NO_START:-0}"
INSTALL_DIR="${CODEAF_INSTALL_DIR:-${AFORGE_INSTALL_DIR:-${HOME}/.codeaf/bin}}" # legacy-name
STATE_ROOT="${CODEAF_HOME:-${AFORGE_HOME:-${HOME}/.codeaf}}" # legacy-name
GITHUB_API="${CODEAF_GITHUB_API:-${AFORGE_GITHUB_API:-https://api.github.com}}" # legacy-name
GITHUB_DOWNLOAD="${CODEAF_GITHUB_DOWNLOAD:-${AFORGE_GITHUB_DOWNLOAD:-https://github.com}}" # legacy-name
# GitHub answers anonymous API calls sixty times an hour per address; a token raises that.
TOKEN="${GITHUB_TOKEN:-${GH_TOKEN:-}}"
TOKEN_NAME="GH_TOKEN"
if [[ -n "${GITHUB_TOKEN:-}" ]]; then
  TOKEN_NAME="GITHUB_TOKEN"
fi
TOKEN_REFUSED=0
API_NOTE=""

usage() {
  cat <<'EOF'
Install codeaf from a GitHub release.

Usage:
  install.sh [--stable|--rc|--dev|--staging] [--version TAG]
             [--name WORD] [--dir PATH] [--no-modify-path] [--no-start]
             [--verbose]

Channels:
  --stable   Latest stable release (default).
  --rc       Latest release candidate.
  --dev      Latest dev channel build.
  --staging  Latest staging channel build.

Flags:
  --version TAG       Install one named release tag.
  --name WORD         Install the binary with this file name.
  --dir PATH          Install somewhere other than ~/.codeaf/bin.
  --no-modify-path    Print the PATH line without editing a shell file or
                      linking the command into a folder already on PATH.
  --no-start          Do not offer to start codeaf when the install ends.
  --verbose           Print download details.
  --help              Show this help.

Environment:
  CHANNEL, VERSION, CODEAF_INSTALL_NAME, CODEAF_INSTALL_DIR
  CODEAF_NO_MODIFY_PATH, CODEAF_NO_START, VERBOSE
  GITHUB_TOKEN or GH_TOKEN: GitHub answers anonymous API calls sixty times an hour per address; a token raises that. A refused token is set aside for one anonymous try.
  CODEAF_GITHUB_API and CODEAF_GITHUB_DOWNLOAD for mirrors and tests
EOF
}

# THE INSTALLER SPEAKS IN ONE VOICE: a mark, a few checked steps, then a short
# guide to the first minute. Colour, the spinner and the non-ASCII marks are for
# a person at a terminal only. Piped, logged or under NO_COLOR
# (https://no-color.org) the same words print plain, so a log or a test reads
# them byte for byte.
init_style() {
  BOLD="" DIM="" ACCENT="" GREEN="" RED="" RESET=""
  MARK_OK="ok" MARK_FAIL="x" MARK_BRAND="*" DOT="-"
  # The frames are an array, not a string sliced a character at a time: slicing
  # by character needs the named locale to be installed, and a UTF-8 LANG that
  # ssh forwarded to a machine without it turns every frame into a broken byte.
  SPINNER_FRAMES=('-' '\' '|' '/')
  local locale="${LC_ALL:-${LC_CTYPE:-${LANG:-}}}"
  case "$locale" in
    *UTF-8*|*utf-8*|*UTF8*|*utf8*)
      MARK_OK="✓" MARK_FAIL="✗" MARK_BRAND="◆" DOT="·"
      SPINNER_FRAMES=('⠋' '⠙' '⠹' '⠸' '⠼' '⠴' '⠦' '⠧' '⠇' '⠏')
      ;;
  esac
  if [[ -t 1 && -z "${NO_COLOR:-}" && "${TERM:-}" != "dumb" ]]; then
    BOLD=$'\033[1m'
    DIM=$'\033[2m'
    GREEN=$'\033[32m'
    RED=$'\033[31m'
    RESET=$'\033[0m'
    # The brand's amber, in truecolor where the terminal says it has it and the
    # nearest of the 256 otherwise.
    case "${COLORTERM:-}" in
      truecolor|24bit) ACCENT=$'\033[38;2;212;162;74m' ;;
      *) ACCENT=$'\033[38;5;178m' ;;
    esac
  fi
}

# A person's home folder reads as ~ in anything printed; the paths written into
# shell files stay absolute.
tidy_path() {
  local path="$1"
  if [[ -n "${HOME:-}" && "$path" == "$HOME"/* ]]; then
    printf '~%s' "${path#"$HOME"}"
  else
    printf '%s' "$path"
  fi
}

# The spinner turns in a background loop while the real work runs in THIS
# shell, so the work's variables (the tag, the HTTP status, the asset name)
# survive it. It draws only on a terminal that has `sleep`, and it is erased
# before the line that replaces it is printed.
SPIN_PID=""
spin_start() {
  [[ -t 1 && -z "${NO_COLOR:-}" && "${TERM:-}" != "dumb" ]] || return 0
  command -v sleep >/dev/null 2>&1 || return 0
  local label="$1"
  printf '\033[?25l'
  (
    local i=0 count=${#SPINNER_FRAMES[@]}
    while :; do
      printf '\r  %s%s%s %s%s%s' "$ACCENT" "${SPINNER_FRAMES[$i]}" "$RESET" "$DIM" "$label" "$RESET"
      i=$(( (i + 1) % count ))
      sleep 0.08
    done
  ) &
  SPIN_PID=$!
}

spin_stop() {
  [[ -n "$SPIN_PID" ]] || return 0
  kill "$SPIN_PID" >/dev/null 2>&1 || true
  wait "$SPIN_PID" 2>/dev/null || true
  SPIN_PID=""
  printf '\r\033[2K\033[?25h'
}

# One finished step: a green check, a word, and what it came to.
step_ok() {
  local word="$1"
  local detail="${2:-}"
  printf '  %s%s%s %-11s %s\n' "$GREEN" "$MARK_OK" "$RESET" "$word" "$detail"
}

print_banner() {
  printf '\n  %s%s%s %scodeaf%s  %sby AgentField AI%s\n\n' \
    "$ACCENT" "$MARK_BRAND" "$RESET" "$BOLD" "$RESET" "$DIM" "$RESET"
}

fail() {
  spin_stop
  if [[ -t 2 && -n "$RED" ]]; then
    printf '  %s%s%s codeaf: %s\n\n' "$RED" "$MARK_FAIL" "$RESET" "$*" >&2
  else
    printf 'codeaf: %s\n' "$*" >&2
  fi
  exit 1
}

# A recovered lookup still explains why the configured token was set aside.
note() {
  if [[ -t 2 && -n "$DIM" ]]; then
    printf '  %s%s%s\n' "$DIM" "$*" "$RESET" >&2
  else
    printf 'codeaf: %s\n' "$*" >&2
  fi
}

usage_error() {
  printf 'codeaf: %s\n\n' "$*" >&2
  usage >&2
  exit 2
}

# The install marker is a local record only: it names how and when this
# machine's codeaf was installed so the usage counts can bucket by channel,
# never who installed it. Nothing here contacts the network.
write_install_marker() {
  local directory="$1"
  local file
  # The channel lands in JSON verbatim and CHANNEL can come from the
  # environment, so only a known channel is recorded; anything else is
  # "unknown" instead of malformed JSON.
  local channel
  case "${CHANNEL:-}" in
    stable|rc|staging|dev) channel="$CHANNEL" ;;
    *) channel="unknown" ;;
  esac
  # The marker is background bookkeeping: a failure skips it without a word.
  if ! mkdir -p "$directory/telemetry" 2>/dev/null; then
    return 0
  fi
  chmod 0700 "$directory/telemetry"
  file="$directory/telemetry/install.json"
  if ! { printf '{"install_method":"script","channel":"%s","installed_at":"%s"}' \
    "$channel" "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"; } > "$file" 2>/dev/null; then
    return 0
  fi
  chmod 0600 "$file"
}

# The guide is the installer's last word: what to type, in the order a person
# types it. The PATH line, when there is one, is step 1 and stands on a line of
# its own, bare, so it can be selected and pasted without trimming a prefix.
# $1 the command a person types (the install name), $2 the PATH line or empty,
# $3 the shell file the line was written to, or empty when none was edited.
print_guide() {
  local command_name="$1"
  local hint="$2"
  local edited="$3"
  local n=1
  printf '\n  %sGet started%s\n\n' "$BOLD" "$RESET"
  if [[ -n "$hint" ]]; then
    if [[ -n "$edited" ]]; then
      printf '  %s%d%s  Open a new terminal, or run this to use %s here:\n' "$ACCENT" "$n" "$RESET" "$command_name"
    else
      printf '  %s%d%s  Put %s on your PATH by adding this to your shell profile:\n' "$ACCENT" "$n" "$RESET" "$command_name"
    fi
    printf '\n     %s%s%s\n\n' "$BOLD" "$hint" "$RESET"
    n=$((n + 1))
  fi
  printf '  %s%d%s  Start it inside any project:\n' "$ACCENT" "$n" "$RESET"
  printf '\n     %scd your-project%s\n' "$BOLD" "$RESET"
  printf '     %s%s%s\n\n' "$BOLD" "$command_name" "$RESET"
  n=$((n + 1))
  printf '  %s%d%s  Connect a model when it asks: OpenRouter signs in through your browser.\n' "$ACCENT" "$n" "$RESET"
  printf '     For your own DeepSeek, Qwen, GLM or Kimi key, or a local Ollama, type\n'
  printf '     /connect in the chat. Then say what you want done, the way you would\n'
  printf '     to a colleague.\n'
  printf '\n  %sMore%s\n\n' "$BOLD" "$RESET"
  printf '     %s%-34s%s %s%s%s\n' "$BOLD" "$command_name do \"add a health check\"" "$RESET" "$DIM" "one task, answer on stdout" "$RESET"
  printf '     %s%-34s%s %s%s%s\n' "$BOLD" "$command_name update" "$RESET" "$DIM" "the newest build, in place" "$RESET"
  printf '     %s%-34s%s %s%s%s\n' "$BOLD" "$command_name --help" "$RESET" "$DIM" "every command" "$RESET"
  printf '\n  %sLearn more%s\n\n' "$BOLD" "$RESET"
  printf '     %s%-10s%s %shttps://agentfield.ai/docs/codeaf%s\n' "$DIM" "Docs" "$RESET" "$ACCENT" "$RESET"
  printf '     %s%-10s%s %shttps://agentfield.ai/docs/codeaf/playbooks%s\n' "$DIM" "Playbooks" "$RESET" "$ACCENT" "$RESET"
  printf '     %s%-10s%s %shttps://agentfield.ai/docs/codeaf/connections%s\n' "$DIM" "Models" "$RESET" "$ACCENT" "$RESET"
  printf '     %s%-10s%s %shttps://discord.gg/aBHaXMkpqh%s\n\n' "$DIM" "Discord" "$RESET" "$ACCENT" "$RESET"
}

init_style

while [[ $# -gt 0 ]]; do
  case "$1" in
    --stable) CHANNEL="stable"; shift ;;
    --rc) CHANNEL="rc"; shift ;;
    --dev) CHANNEL="dev"; shift ;;
    --staging) CHANNEL="staging"; shift ;;
    --version)
      [[ $# -ge 2 ]] || usage_error "--version needs a tag"
      VERSION="$2"
      shift 2
      ;;
    --name)
      [[ $# -ge 2 ]] || usage_error "--name needs a word"
      INSTALL_NAME="$2"
      shift 2
      ;;
    --dir)
      [[ $# -ge 2 ]] || usage_error "--dir needs a path"
      INSTALL_DIR="$2"
      shift 2
      ;;
    --no-modify-path) NO_MODIFY_PATH=1; shift ;;
    --no-start) NO_START=1; shift ;;
    --verbose|-v) VERBOSE=1; shift ;;
    --help|-h) usage; exit 0 ;;
    *) usage_error "unknown option: $1" ;;
  esac
done

case "$CHANNEL" in
  stable|rc|dev|staging) ;;
  *) usage_error "CHANNEL must be stable, rc, dev, or staging" ;;
esac

name_pattern='^[A-Za-z0-9][A-Za-z0-9._-]*$'
if [[ ! "$INSTALL_NAME" =~ $name_pattern ]]; then
  usage_error "--name / CODEAF_INSTALL_NAME must match ^[A-Za-z0-9][A-Za-z0-9._-]*$"
fi

if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
  fail "curl or wget is required"
fi

TMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/codeaf.XXXXXX")
INSTALL_TEMP=""
cleanup() {
  spin_stop
  rm -rf "$TMP_ROOT"
  if [[ -n "$INSTALL_TEMP" ]]; then
    rm -f "$INSTALL_TEMP"
  fi
}
trap cleanup EXIT HUP INT TERM

print_banner

HTTP_STATUS=""
HTTP_HEADERS=""
HTTP_BODY=""
WGET_KEEPS_ERRORS=""
http_get() {
  local url="$1"
  local destination="$2"
  local accept="${3:-application/vnd.github+json}"
  local authenticate="${4:-0}"
  local status
  HTTP_HEADERS="$destination.headers"
  HTTP_BODY="$destination"
  # A transport failure must not leave a previous response looking current.
  : > "$HTTP_HEADERS"
  : > "$destination"
  if [[ "$VERBOSE" == "1" ]]; then
    printf 'codeaf: GET %s\n' "$url"
  fi
  if command -v curl >/dev/null 2>&1; then
    local args=(-sSL -D "$HTTP_HEADERS" --output "$destination" --write-out '%{http_code}' -H "Accept: ${accept}")
    if [[ "$authenticate" == "1" && -n "$TOKEN" ]]; then
      args+=(-H "Authorization: Bearer ${TOKEN}")
    fi
    if ! status=$(curl "${args[@]}" "$url"); then
      HTTP_STATUS=""
      return 1
    fi
    HTTP_STATUS="$status"
    case "$status" in
      2*) return 0 ;;
      *) return 1 ;;
    esac
  fi

  local wget_code
  local args=(-O "$destination" -S --header="Accept: ${accept}")
  if [[ "$authenticate" == "1" && -n "$TOKEN" ]]; then
    args+=(--header="Authorization: Bearer ${TOKEN}")
  fi
  # BusyBox lacks this flag, so asking first keeps its successful downloads working.
  # The help is read whole rather than piped to grep -q: under pipefail an early
  # grep exit can kill wget mid-write and quietly read as "not supported".
  if [[ -z "$WGET_KEEPS_ERRORS" ]]; then
    WGET_KEEPS_ERRORS=0
    case "$(wget --help 2>&1 || true)" in
      *--content-on-error*) WGET_KEEPS_ERRORS=1 ;;
    esac
  fi
  if [[ "$WGET_KEEPS_ERRORS" == "1" ]]; then
    args+=(--content-on-error)
  fi
  if wget "${args[@]}" "$url" 2> "$HTTP_HEADERS"; then
    wget_code=0
  else
    wget_code=$?
  fi
  status=$(awk '$1 ~ /^HTTP\/[0-9.]+$/ && $2 ~ /^[0-9][0-9][0-9]$/ {status=$2} END {print status}' "$HTTP_HEADERS")
  HTTP_STATUS="$status"
  if [[ "$wget_code" == "0" && "$status" == 2* ]]; then
    return 0
  fi
  return 1
}

# BEGIN api refusal: pure functions, read by internal/release/install_test.go
api_header() {
  # Redirects and wget indentation must not let an earlier quota shadow the final answer.
  LC_ALL=C awk -v wanted="$2" '
    {
      sub(/^[ \t]+/, "")
      sub(/\r$/, "")
      if ($0 ~ /^HTTP\/[0-9.]+[ \t]+[0-9][0-9][0-9]/) {
        value = ""
      } else {
        colon = index($0, ":")
        if (colon && tolower(substr($0, 1, colon - 1)) == wanted) {
          value = substr($0, colon + 1)
          sub(/^[ \t]+/, "", value)
          sub(/[ \t]+$/, "", value)
        }
      }
    }
    END {print value}
  ' "$1"
}

api_reason() {
  local status="$1" headers="$2" body="$3"
  case "$status" in
    "") printf 'unreachable' ;;
    401) printf 'rejected' ;;
    429) printf 'quota' ;;
    403)
      if [[ "$(api_header "$headers" x-ratelimit-remaining)" == "0" ]] || LC_ALL=C grep -qi 'rate limit' "$body" 2>/dev/null; then
        printf 'quota'
      else
        printf 'access'
      fi
      ;;
    404) printf 'missing' ;;
    5??) printf 'server' ;;
    *) printf 'other' ;;
  esac
}

api_epoch() {
  local epoch="$1" rendered pattern='^[0-9][0-9]:[0-9][0-9]' digits='^[0-9]{1,11}$'
  # An epoch too long for any clock is refused here: bash's printf fallback
  # would otherwise render it as the epoch's first second and invent a time.
  [[ "$epoch" =~ $digits ]] || return 1
  # A missing date is legal and keeps the conservative wait on minimal hosts.
  command -v date >/dev/null 2>&1 || return 1
  # Each host gets its own date syntax; bash supplies the last option only where it exists.
  # A BSD date once read -d as "set the kernel's daylight-saving flag", so it is
  # never offered there, even though the attempt would fail without root.
  case "$(uname -s 2>/dev/null)" in
    Darwin|*BSD|DragonFly) ;;
    *)
      if rendered=$(date -d "@$epoch" '+%H:%M %Z' 2>/dev/null) && [[ "$rendered" =~ $pattern ]]; then
        printf '%s' "${rendered% }"
        return 0
      fi
      ;;
  esac
  if rendered=$(date -r "$epoch" '+%H:%M %Z' 2>/dev/null) && [[ "$rendered" =~ $pattern ]]; then
    printf '%s' "${rendered% }"
    return 0
  fi
  if (( BASH_VERSINFO[0] > 4 || (BASH_VERSINFO[0] == 4 && BASH_VERSINFO[1] >= 2) )); then
    if rendered=$(printf '%(%H:%M %Z)T' "$epoch" 2>/dev/null) && [[ "$rendered" =~ $pattern ]]; then
      printf '%s' "${rendered% }"
      return 0
    fi
  fi
  return 1
}

api_wait() {
  # Six digits is eleven days; a longer Retry-After overflows some awks into
  # negative minutes, so it reads as no number at all.
  local headers="$1" reset rendered retry digits='^[0-9]{1,6}$'
  reset=$(api_header "$headers" x-ratelimit-reset)
  if [[ -n "$reset" ]] && rendered=$(api_epoch "$reset"); then
    printf 'until %s' "$rendered"
    return 0
  fi
  retry=$(api_header "$headers" retry-after)
  if [[ "$retry" =~ $digits ]]; then
    LC_ALL=C awk -v seconds="$retry" 'BEGIN {
      if (seconds <= 90) printf "for about a minute"
      else printf "for about %.0f minutes", int((seconds + 59) / 60)
    }'
  else
    printf 'for up to an hour'
  fi
}

api_message() {
  # Only a bounded, printable message reaches the terminal, never the raw response body.
  LC_ALL=C awk '
    {json = json $0 "\n"}
    END {
      # Spelled out rather than [[:space:]]: mawk 1.3.3 has no character classes.
      if (!match(json, /"message"[ \t\r\n]*:[ \t\r\n]*"/)) exit
      json = substr(json, RSTART + RLENGTH)
      message = ""
      for (i = 1; i <= length(json); i++) {
        c = substr(json, i, 1)
        if (c == "\\") {
          c = substr(json, ++i, 1)
          if (c == "u") {
            code = 0
            for (j = 1; j <= 4; j++) {
              digit = index("0123456789abcdef", tolower(substr(json, ++i, 1))) - 1
              if (digit < 0) exit
              code = code * 16 + digit
            }
            c = ""
            if (code >= 32 && code <= 126) c = sprintf("%c", code)
          } else if (c != "\\" && c != "\"" && c != "/") {
            c = ""
          }
        } else if (c == "\"") {
          break
        }
        if (c ~ /^[ -~]$/) message = message c
      }
      gsub(/ +/, " ", message)
      if (length(message) > 100) message = substr(message, 1, 100) "..."
      printf "%s", message
    }
  ' "$1"
}
# END api refusal

api_status_message() {
  local status="$1" message
  message=$(api_message "$2")
  printf '%s' "$status"
  [[ -z "$message" ]] || printf ': %s' "$message"
}

api_short() {
  local status="$1" headers="$2" body="$3" reason
  reason=$(api_reason "$status" "$headers" "$body")
  case "$reason" in
    quota) printf "this address's rate limit is used up %s" "$(api_wait "$headers")" ;;
    unreachable) printf 'GitHub could not be reached' ;;
    missing|server) printf '%s' "$status" ;;
    *) api_status_message "$status" "$body" ;;
  esac
}

api_problem() {
  local reason message detail host token_sentence advice
  reason=$(api_reason "$HTTP_STATUS" "$HTTP_HEADERS" "$HTTP_BODY")
  if [[ "$TOKEN_REFUSED" == "1" ]]; then
    case "$(api_reason "$TOKEN_STATUS" "$TOKEN_HEADERS" "$TOKEN_BODY")" in
      rejected)
        token_sentence="GitHub rejected $TOKEN_NAME ($(api_status_message "$TOKEN_STATUS" "$TOKEN_BODY"))"
        advice="replace or unset $TOKEN_NAME"
        ;;
      quota)
        token_sentence="GitHub's API rate limit for $TOKEN_NAME is used up $(api_wait "$TOKEN_HEADERS")"
        advice="wait"
        ;;
      access)
        token_sentence="GitHub refused $TOKEN_NAME access to $REPOSITORY ($(api_status_message "$TOKEN_STATUS" "$TOKEN_BODY"))"
        advice="check what $TOKEN_NAME may read"
        ;;
    esac
    fail "$token_sentence, and the request without it failed too ($(api_short "$HTTP_STATUS" "$HTTP_HEADERS" "$HTTP_BODY")); $advice, or pin VERSION=<tag>"
  fi
  detail=$(api_status_message "$HTTP_STATUS" "$HTTP_BODY")
  case "$reason" in
    quota) fail "GitHub's API rate limit for this address is used up $(api_wait "$HTTP_HEADERS"); export GITHUB_TOKEN to use your own, or pin VERSION=<tag>" ;;
    access) fail "GitHub's API refused access to $REPOSITORY ($detail); pin VERSION=<tag>" ;;
    missing)
      if [[ -n "$TOKEN" ]]; then
        fail "GitHub's API cannot find $REPOSITORY with $TOKEN_NAME (404), which is also its answer for a repository the token cannot read; check what $TOKEN_NAME may read, or pin VERSION=<tag>"
      fi
      fail "GitHub's API cannot find $REPOSITORY (404), which is also its answer for a repository you cannot read; export GITHUB_TOKEN, or pin VERSION=<tag>"
      ;;
    rejected) fail "GitHub's API refused the request ($detail); pin VERSION=<tag>" ;;
    server) fail "GitHub's API answered $HTTP_STATUS and may be having trouble; try again in a minute, or pin VERSION=<tag>" ;;
    unreachable)
      host="${GITHUB_API#*://}"
      host="${host%%/*}"
      fail "GitHub's API could not be reached at $host; check the connection, or pin VERSION=<tag>"
      ;;
    other)
      message=$(api_message "$HTTP_BODY")
      if [[ -n "$message" ]]; then
        fail "GitHub's API answered $HTTP_STATUS ($message); pin VERSION=<tag>"
      fi
      fail "GitHub's API answered $HTTP_STATUS; pin VERSION=<tag>"
      ;;
  esac
}

release_api_get() {
  local suffix="$1" destination="$2" authenticate=0 short
  if [[ -n "$TOKEN" && "$TOKEN_REFUSED" == "0" ]]; then
    authenticate=1
  fi
  if http_get "$GITHUB_API/repos/$REPOSITORY/$suffix" "$destination" "application/vnd.github+json" "$authenticate"; then
    return 0
  fi
  if [[ "$authenticate" != "1" ]]; then
    return 1
  fi
  case "$HTTP_STATUS" in
    401|403|429) ;;
    *) return 1 ;;
  esac
  # THE REFUSED TOKEN STAYS ASIDE: a second lookup must not spend its bucket again.
  TOKEN_REFUSED=1
  TOKEN_STATUS="$HTTP_STATUS"
  TOKEN_HEADERS="$TMP_ROOT/token.headers"
  TOKEN_BODY="$TMP_ROOT/token.json"
  cp "$HTTP_HEADERS" "$TOKEN_HEADERS"
  cp "$HTTP_BODY" "$TOKEN_BODY"
  if ! http_get "$GITHUB_API/repos/$REPOSITORY/$suffix" "$destination" "application/vnd.github+json" 0; then
    return 1
  fi
  if [[ "$(api_reason "$TOKEN_STATUS" "$TOKEN_HEADERS" "$TOKEN_BODY")" == "quota" ]]; then
    short="its rate limit is used up $(api_wait "$TOKEN_HEADERS")"
  else
    short=$(api_status_message "$TOKEN_STATUS" "$TOKEN_BODY")
  fi
  API_NOTE="GitHub refused $TOKEN_NAME ($short); continuing without it"
}

extract_tags() {
  grep -Eo '"tag_name"[[:space:]]*:[[:space:]]*"[^"]+"' "$1" |
    sed -E 's/^"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)"$/\1/'
}

# Channel selection accepts exactly the grammar used by internal/update/version.go.
tag_matches_channel() {
  local channel="$1" tag="$2" pattern
  case "$channel" in
    stable) pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$' ;;
    rc) pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-rc\.([1-9][0-9]*)$' ;;
    dev) pattern='^dev-[0-9]{8}-[0-9a-f]{12}$' ;;
    staging) pattern='^staging-[0-9]{8}-[0-9a-f]{12}$' ;;
    *) return 1 ;;
  esac
  [[ "$tag" =~ $pattern ]]
}

# GitHub's release-list order is not publish order, so the LAW carries each tag's own timestamp into channel selection.
extract_dated_tags() {
  awk '
    {
      if (NR > 1) {
        json = json "\n"
      }
      json = json $0
    }
    END {
      tag_pattern = "\"tag_name\"[ \t]*:[ \t]*\"[^\"]*\""
      published_pattern = "\"published_at\"[ \t]*:[ \t]*\"[^\"]*\""
      created_pattern = "\"created_at\"[ \t]*:[ \t]*\"[^\"]*\""
      while (match(json, tag_pattern)) {
        tag_field = substr(json, RSTART, RLENGTH)
        tail = substr(json, RSTART + RLENGTH)
        if (match(tail, tag_pattern)) {
          segment = substr(tail, 1, RSTART - 1)
          json = substr(tail, RSTART)
        } else {
          segment = tail
          json = ""
        }

        tag = tag_field
        sub(/^"tag_name"[ \t]*:[ \t]*"/, "", tag)
        sub(/"$/, "", tag)

        # Only fields before assets belong to the release itself.
        release_fields = segment
        if (match(release_fields, /"assets"[ \t]*:/)) {
          release_fields = substr(release_fields, 1, RSTART - 1)
        }
        published = ""
        if (match(release_fields, published_pattern)) {
          published = substr(release_fields, RSTART, RLENGTH)
          sub(/^"published_at"[ \t]*:[ \t]*"/, "", published)
          sub(/"$/, "", published)
        }
        created = ""
        if (match(release_fields, created_pattern)) {
          created = substr(release_fields, RSTART, RLENGTH)
          sub(/^"created_at"[ \t]*:[ \t]*"/, "", created)
          sub(/"$/, "", created)
        }
        stamp = published
        if (stamp == "") {
          stamp = created
        }
        if (stamp == "") {
          stamp = "-"
        }
        print stamp " " tag
      }
    }
  ' "$1"
}

# --verbose prints each GET as it goes, which a spinner would draw over.
spin_label() {
  [[ "$VERBOSE" == "1" ]] || spin_start "$1"
}

release_file="$TMP_ROOT/release.json"
if [[ -z "$VERSION" ]]; then
  spin_label "Finding the latest $CHANNEL build"
fi
if [[ -n "$VERSION" ]]; then
  TAG="$VERSION"
else
  case "$CHANNEL" in
    stable)
      if ! release_api_get "releases/latest" "$release_file"; then
        if [[ "$HTTP_STATUS" == "404" && "$TOKEN_REFUSED" == "0" ]]; then
          fail "no stable build has been published yet"
        fi
        api_problem
      fi
      TAG=$(extract_tags "$release_file" | sed -n '1p' || true)
      ;;
    rc|dev|staging)
      list_file="$TMP_ROOT/releases.json"
      if ! release_api_get "releases?per_page=100" "$list_file"; then
        api_problem
      fi
      TAG=""
      TAG_STAMP=""
      # GitHub's list order is not publish order, so the LAW chooses a matching candidate by its own release timestamp.
      while IFS=' ' read -r stamp candidate; do
        if tag_matches_channel "$CHANNEL" "$candidate"; then
          if [[ -z "$TAG" || "$stamp" > "$TAG_STAMP" ]]; then
            TAG="$candidate"
            TAG_STAMP="$stamp"
          fi
        fi
      done < <(extract_dated_tags "$list_file")
      if [[ -z "$TAG" ]]; then
        fail "no $CHANNEL build has been published yet"
      fi
      ;;
  esac
fi

spin_stop
if [[ -n "$API_NOTE" ]]; then
  note "$API_NOTE"
fi
if [[ -z "${TAG:-}" ]]; then
  fail "GitHub's API answered without a release tag; pin VERSION=<tag>"
fi

if [[ -z "$VERSION" ]] && ! tag_matches_channel "$CHANNEL" "$TAG"; then
  fail "no $CHANNEL build has been published yet"
fi

if tag_matches_channel "dev" "$TAG"; then
  DISPLAY_CHANNEL="dev"
elif tag_matches_channel "staging" "$TAG"; then
  DISPLAY_CHANNEL="staging"
elif tag_matches_channel "rc" "$TAG"; then
  DISPLAY_CHANNEL="rc"
elif tag_matches_channel "stable" "$TAG"; then
  DISPLAY_CHANNEL="stable"
else
  DISPLAY_CHANNEL=""
fi

system=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$system" in
  darwin) OS="darwin" ;;
  linux) OS="linux" ;;
  mingw*|msys*|cygwin*) OS="windows" ;;
  *) fail "unsupported platform: $system/$(uname -m)" ;;
esac

machine=$(uname -m | tr '[:upper:]' '[:lower:]')
case "$machine" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) fail "unsupported platform: $OS/$machine" ;;
esac

extension=""
if [[ "$OS" == "windows" ]]; then
  extension=".exe"
fi
ASSET="codeaf-${OS}-${ARCH}${extension}"

download_asset() {
	local repository="$1"
	local name="$2"
	local destination="$3"
	http_get "$GITHUB_DOWNLOAD/$repository/releases/download/$TAG/$name" "$destination" "application/octet-stream"
}

download_release() {
	local repository="$1"
	ASSET="codeaf-${OS}-${ARCH}${extension}"
	if ! download_asset "$repository" "$ASSET" "$TMP_ROOT/$ASSET"; then
		LEGACY_ASSET="aforge-${OS}-${ARCH}${extension}" # Remove after releases with the former asset name age out. # legacy-name
		if [[ "$HTTP_STATUS" != "404" ]] || ! download_asset "$repository" "$LEGACY_ASSET" "$TMP_ROOT/$LEGACY_ASSET"; then
			return 1
		fi
		ASSET="$LEGACY_ASSET"
	fi
	download_asset "$repository" "checksums.txt" "$TMP_ROOT/checksums.txt"
}

# The channel and tag are not announced on a normal run: the installed
# binary names itself at the end, and that one line is the whole receipt.
if [[ "$VERBOSE" == "1" ]]; then
	if [[ -n "$DISPLAY_CHANNEL" ]]; then
		printf 'codeaf: %s %s for %s/%s\n' "$DISPLAY_CHANNEL" "$TAG" "$OS" "$ARCH" >&2
	else
		printf 'codeaf: %s for %s/%s\n' "$TAG" "$OS" "$ARCH" >&2
	fi
fi
spin_label "Downloading codeaf $TAG for $OS/$ARCH"
if ! download_release "$REPOSITORY"; then
	fail "no codeaf-${OS}-${ARCH}${extension} in release ${TAG}; check the tag on the Releases page"
fi

expected=$(awk -v name="$ASSET" '$2 == name || $2 == "*" name {print $1; exit}' "$TMP_ROOT/checksums.txt")
[[ -n "$expected" ]] || fail "checksums.txt has no checksum for $ASSET"
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$TMP_ROOT/$ASSET" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  actual=$(shasum -a 256 "$TMP_ROOT/$ASSET" | awk '{print $1}')
else
  fail "sha256sum or shasum is required to check the download"
fi
if [[ "$actual" != "$expected" ]]; then
  fail "the checksum for $ASSET did not match"
fi
spin_stop
if [[ -n "$DISPLAY_CHANNEL" ]]; then
  step_ok "Downloaded" "$DISPLAY_CHANNEL build for $OS/$ARCH ${DOT} checksum verified"
else
  step_ok "Downloaded" "$TAG for $OS/$ARCH ${DOT} checksum verified"
fi

# Running the verified binary before creating its destination gives boot
# adoption its one chance to move an existing state root. A custom install
# elsewhere must not mutate the login's state folders.
RUN_BOOT_ADOPTION=0
case "$INSTALL_DIR/" in
  "$STATE_ROOT/"*)
    RUN_BOOT_ADOPTION=1
    chmod 0755 "$TMP_ROOT/$ASSET"
    adoption_status=0
    "$TMP_ROOT/$ASSET" version >/dev/null 2>&1 || adoption_status=$?
    if [[ "$adoption_status" != "0" && "$VERBOSE" == "1" ]]; then
      printf 'codeaf: pre-install adoption exited %s; continuing\n' "$adoption_status" >&2
    fi
    ;;
esac

mkdir -p "$INSTALL_DIR"
INSTALL_TEMP="$INSTALL_DIR/.$INSTALL_NAME.tmp.$$"
cp "$TMP_ROOT/$ASSET" "$INSTALL_TEMP"
chmod 0755 "$INSTALL_TEMP"
mv -f "$INSTALL_TEMP" "$INSTALL_DIR/$INSTALL_NAME${extension}"
INSTALL_TEMP=""
if [[ "$VERBOSE" == "1" ]]; then
  printf 'codeaf: installed %s\n' "$INSTALL_DIR/$INSTALL_NAME${extension}" >&2
fi

# The receipt is the installed binary naming itself: `codeaf version` is one
# line by law, so it reads whole after "Installed". A FILE INSTALLED UNDER
# ANOTHER NAME IS NAMED FIRST, because the receipt tells a person what to type
# next: a devaf install that said "Installed codeaf …" sent them to a command
# this install never wrote, or to an older codeaf that happened to be on their
# PATH. The version line after it stays whole, so the build is still named and
# codeaf is still the product.
if [[ "$RUN_BOOT_ADOPTION" == "1" ]]; then
  version_line=$("$INSTALL_DIR/$INSTALL_NAME${extension}" version)
else
  version_line=$(CODEAF_HOME="$STATE_ROOT" "$INSTALL_DIR/$INSTALL_NAME${extension}" version)
fi
# The line is split where it would wrap an 80-column terminal: the name and the
# tag on the step, the rest of the build's own words dim beneath it, whole.
version_head="${version_line%% built *}"
version_rest=""
if [[ "$version_head" != "$version_line" ]]; then
  version_rest="built ${version_line#* built }"
elif [[ "$version_line" == *" · "* ]]; then
  version_head="${version_line%% · *}"
  version_rest="${version_line#* · }"
fi
if [[ "$INSTALL_NAME" == "codeaf" ]]; then
  step_ok "Installed" "$version_head"
else
  step_ok "Installed" "$INSTALL_NAME · $version_head"
fi
if [[ -n "$version_rest" ]]; then
  printf '  %-13s %s%s%s\n' "" "$DIM" "$version_rest" "$RESET"
fi
printf '  %-13s %s%s%s\n' "" "$DIM" "$(tidy_path "$INSTALL_DIR/$INSTALL_NAME${extension}")" "$RESET"

path_has_dir() {
  case ":${PATH}:" in
    *":$INSTALL_DIR:"*) return 0 ;;
    *) return 1 ;;
  esac
}

append_path_line() {
  local file="$1"
  local line="$2"
  local former_marker='# aforge installer' # legacy-name
  mkdir -p "$(dirname "$file")"
  if [[ -f "$file" ]] && { grep -F '# codeaf installer' "$file" >/dev/null 2>&1 || grep -F "$former_marker" "$file" >/dev/null 2>&1; }; then
    local repaired="$file.codeaf-path.$$"
    awk -v line="$line" -v current='# codeaf installer' -v former="$former_marker" '
      index($0, current) || index($0, former) { if (!done) { print line; done=1 }; next }
      { print }
      END { if (!done) print line }
    ' "$file" > "$repaired"
    mv -f "$repaired" "$file"
  else
    printf '%s\n' "$line" >> "$file"
  fi
}

# The folders a link may go in, in order: the person's own first, then the
# system's customary one when they can write to it without sudo. A folder counts
# only when it is on PATH already. NOTHING THERE IS REPLACED BUT THIS INSTALL'S
# OWN LINK: a file, or a link to anything else (a source build, another install,
# a leftover pointing nowhere), is somebody's choice, and shadowing it silently
# is how a person ends up running a build they did not choose. The paste line is
# the answer then. Prints the folder it linked into.
link_into_path() {
  local target="$INSTALL_DIR/$INSTALL_NAME${extension}"
  local dir
  for dir in "$HOME/.local/bin" "$HOME/bin" "/usr/local/bin"; do
    case ":${PATH}:" in
      *":$dir:"*) ;;
      *) continue ;;
    esac
    [[ -d "$dir" && -w "$dir" ]] || continue
    if [[ -L "$dir/$INSTALL_NAME" ]]; then
      # -ef follows the link: true only when it lands on this very file. It is
      # a shell test, so the installer needs no readlink.
      [[ "$dir/$INSTALL_NAME" -ef "$target" ]] || return 1
    elif [[ -e "$dir/$INSTALL_NAME" ]]; then
      return 1
    fi
    if ln -sf "$target" "$dir/$INSTALL_NAME" 2>/dev/null; then
      printf '%s' "$dir"
      return 0
    fi
  done
  return 1
}

# The PATH line is not printed here. It is step 1 of the guide at the end, so
# the one line a person has to paste sits beside the words that say why.
PATH_HINT=""
PATH_FILE=""
if [[ "$OS" != "windows" ]] && ! path_has_dir; then
  export_line="export PATH=\"$INSTALL_DIR:\$PATH\""
  PATH_HINT="$export_line"
  # The line a person reads spells their home as $HOME, which is shorter and
  # still right if they copy it into a profile on another machine.
  if [[ -n "${HOME:-}" && "$INSTALL_DIR" == "$HOME"/* ]]; then
    PATH_HINT="export PATH=\"\$HOME${INSTALL_DIR#"$HOME"}:\$PATH\""
  fi
  shell_name=$(basename "${SHELL:-/bin/bash}")
  if [[ "$shell_name" == "fish" ]]; then
    PATH_HINT="fish_add_path \"$INSTALL_DIR\""
  fi
  if [[ "$NO_MODIFY_PATH" != "1" ]]; then
    case "$shell_name" in
      zsh)
        PATH_FILE="$HOME/.zshrc"
        append_path_line "$PATH_FILE" "$export_line # codeaf installer"
        ;;
      fish)
        PATH_FILE="$HOME/.config/fish/config.fish"
        append_path_line "$PATH_FILE" "fish_add_path \"$INSTALL_DIR\" # codeaf installer"
        ;;
      *)
        PATH_FILE="$HOME/.bashrc"
        append_path_line "$PATH_FILE" "$export_line # codeaf installer"
        if [[ "$OS" == "darwin" && -f "$HOME/.bash_profile" ]]; then
          append_path_line "$HOME/.bash_profile" "$export_line # codeaf installer"
        fi
        ;;
    esac
    step_ok "PATH" "added to $(tidy_path "$PATH_FILE")"
    # A piped install cannot change the PATH of the shell that ran it, so the
    # profile line only reaches the NEXT terminal. A link in a folder that is
    # already on PATH makes the command work in this one, with nothing to paste.
    # The link only helps when it is what the name resolves to: an older
    # codeaf earlier on PATH would still answer, so the paste line stays.
    if link_dir=$(link_into_path) && [[ "$(type -P "$INSTALL_NAME" 2>/dev/null)" == "$link_dir/$INSTALL_NAME" ]]; then
      step_ok "Linked" "$(tidy_path "$link_dir/$INSTALL_NAME"), ready in this terminal"
      PATH_HINT=""
    fi
  fi
fi

# The install marker lives under the state root, and a custom install outside
# it must not create the login's state folders: the marker is written when the
# install is inside the state root or the root already exists, and skipped
# otherwise (the binary then reports install_method "unknown").
if [[ "$RUN_BOOT_ADOPTION" == "1" || -d "$STATE_ROOT" ]]; then
  write_install_marker "$STATE_ROOT"
fi
print_guide "$INSTALL_NAME" "$PATH_HINT" "$PATH_FILE"

# Starting codeaf here only helps when here is a project. The home folder and /
# are where a piped install usually runs, and neither is one: starting there
# would hand codeaf the whole of it, and the guide above already says to cd into
# a project first. A folder deleted from under the shell is no place either.
start_folder() {
  local here home
  here=$(pwd -P 2>/dev/null) || return 1
  home=$(cd "${HOME:-/}" 2>/dev/null && pwd -P) || home="${HOME:-}"
  [[ "$here" != "/" && "$here" != "$home" ]]
}

# The last step is offered, not taken: a person at a terminal is asked whether
# to start codeaf now, where the first run connects a model, and Enter says yes.
# The answer is read from the terminal itself, because under `curl | bash`
# standard input is the script. Nothing is asked of a pipe, a CI runner, a
# --verbose run or --no-start, nor in the home folder or /.
offer_start() {
  [[ "$NO_START" != "1" && "$VERBOSE" != "1" && -z "${CI:-}" && "$OS" != "windows" ]] || return 0
  [[ -t 1 ]] || return 0
  start_folder || return 0
  { : </dev/tty; } 2>/dev/null || return 0
  local reply=""
  printf '  %sStart %s in %s now?%s %s[Y/n]%s ' "$BOLD" "$INSTALL_NAME" "$(tidy_path "$PWD")" "$RESET" "$DIM" "$RESET"
  read -r reply </dev/tty || reply="n"
  case "$reply" in
    ""|y|Y|yes|Yes|YES) ;;
    *) printf '\n'; return 0 ;;
  esac
  printf '\n'
  cleanup
  trap - EXIT HUP INT TERM
  exec "$INSTALL_DIR/$INSTALL_NAME${extension}" </dev/tty
}
offer_start

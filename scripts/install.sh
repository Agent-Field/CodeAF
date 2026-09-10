#!/usr/bin/env bash

set -euo pipefail

REPOSITORY="Agent-Field/aforge-v2"
CHANNEL="${CHANNEL:-stable}"
VERSION="${VERSION:-}"
VERBOSE="${VERBOSE:-0}"
NO_MODIFY_PATH="${AFORGE_NO_MODIFY_PATH:-0}"
INSTALL_DIR="${AFORGE_INSTALL_DIR:-${HOME}/.aforge/bin}"
GITHUB_API="${AFORGE_GITHUB_API:-https://api.github.com}"
GITHUB_DOWNLOAD="${AFORGE_GITHUB_DOWNLOAD:-https://github.com}"
TOKEN="${GITHUB_TOKEN:-${GH_TOKEN:-}}"

usage() {
  cat <<'EOF'
Install aforge from a GitHub release.

Usage:
  install.sh [--stable|--rc|--dev|--staging] [--version TAG]
             [--dir PATH] [--no-modify-path] [--verbose]

Channels:
  --stable   Latest stable release (default).
  --rc       Latest release candidate.
  --dev      Latest dev channel build.
  --staging  Latest staging channel build.

Flags:
  --version TAG       Install one named release tag.
  --dir PATH          Install somewhere other than ~/.aforge/bin.
  --no-modify-path    Print the PATH line without editing a shell file.
  --verbose           Print download details.
  --help              Show this help.

Environment:
  CHANNEL, VERSION, AFORGE_INSTALL_DIR, AFORGE_NO_MODIFY_PATH, VERBOSE
  GITHUB_TOKEN or GH_TOKEN for a private repository
  AFORGE_GITHUB_API and AFORGE_GITHUB_DOWNLOAD for mirrors and tests
EOF
}

fail() {
  printf 'aforge: %s\n' "$*" >&2
  exit 1
}

usage_error() {
  printf 'aforge: %s\n\n' "$*" >&2
  usage >&2
  exit 2
}

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
    --dir)
      [[ $# -ge 2 ]] || usage_error "--dir needs a path"
      INSTALL_DIR="$2"
      shift 2
      ;;
    --no-modify-path) NO_MODIFY_PATH=1; shift ;;
    --verbose|-v) VERBOSE=1; shift ;;
    --help|-h) usage; exit 0 ;;
    *) usage_error "unknown option: $1" ;;
  esac
done

case "$CHANNEL" in
  stable|rc|dev|staging) ;;
  *) usage_error "CHANNEL must be stable, rc, dev, or staging" ;;
esac

if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
  fail "curl or wget is required"
fi

TMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/aforge.XXXXXX")
INSTALL_TEMP=""
cleanup() {
  rm -rf "$TMP_ROOT"
  if [[ -n "$INSTALL_TEMP" ]]; then
    rm -f "$INSTALL_TEMP"
  fi
}
trap cleanup EXIT HUP INT TERM

HTTP_STATUS=""
http_get() {
  local url="$1"
  local destination="$2"
  local accept="${3:-application/vnd.github+json}"
  local status
  if [[ "$VERBOSE" == "1" ]]; then
    printf 'aforge: GET %s\n' "$url"
  fi
  if command -v curl >/dev/null 2>&1; then
    local args=(-sSL --output "$destination" --write-out '%{http_code}' -H "Accept: ${accept}")
    if [[ -n "$TOKEN" ]]; then
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

  local headers="$TMP_ROOT/http.headers"
  local wget_code
  local args=(-O "$destination" -S --header="Accept: ${accept}")
  if [[ -z "$TOKEN" ]]; then
    if wget "${args[@]}" "$url" 2> "$headers"; then
      wget_code=0
    else
      wget_code=$?
    fi
  else
    args+=(--max-redirect=0 --header="Authorization: Bearer ${TOKEN}")
    if wget "${args[@]}" "$url" 2> "$headers"; then
      wget_code=0
    else
      wget_code=$?
    fi
    status=$(awk '$1 ~ /^HTTP\/[0-9.]+$/ && $2 ~ /^[0-9][0-9][0-9]$/ {status=$2} END {print status}' "$headers")
    if [[ "$status" == 3* ]]; then
      local location
      location=$(awk 'tolower($1) == "location:" {value=$2; sub(/\r$/, "", value); print value; exit}' "$headers")
      [[ -n "$location" ]] || { HTTP_STATUS="$status"; return 1; }
      # AN AUTHORIZATION HEADER NEVER FOLLOWS A REDIRECT. GitHub's asset
      # destination carries signed credentials of its own, while wget would
      # otherwise forward the repository token to a different host.
      headers="$TMP_ROOT/http.redirect.headers"
      args=(-O "$destination" -S --header="Accept: ${accept}")
      if wget "${args[@]}" "$location" 2> "$headers"; then
        wget_code=0
      else
        wget_code=$?
      fi
    fi
  fi
  status=$(awk '$1 ~ /^HTTP\/[0-9.]+$/ && $2 ~ /^[0-9][0-9][0-9]$/ {status=$2} END {print status}' "$headers")
  HTTP_STATUS="$status"
  if [[ "$wget_code" == "0" && "$status" == 2* ]]; then
    return 0
  fi
  return 1
}

api_problem() {
  fail "GitHub could not be reached; export GITHUB_TOKEN or pin VERSION=<tag> and try again"
}

extract_tags() {
  grep -Eo '"tag_name"[[:space:]]*:[[:space:]]*"[^"]+"' "$1" |
    sed -E 's/^"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)"$/\1/'
}

release_file="$TMP_ROOT/release.json"
if [[ -n "$VERSION" ]]; then
  if ! http_get "$GITHUB_API/repos/$REPOSITORY/releases/tags/$VERSION" "$release_file"; then
    if [[ "$HTTP_STATUS" == "404" ]]; then
      fail "release $VERSION was not found; see $GITHUB_DOWNLOAD/$REPOSITORY/releases"
    fi
    api_problem
  fi
  TAG="$VERSION"
else
  case "$CHANNEL" in
    stable)
      if ! http_get "$GITHUB_API/repos/$REPOSITORY/releases/latest" "$release_file"; then
        if [[ "$HTTP_STATUS" == "404" ]]; then
          fail "no stable build has been published yet"
        fi
        api_problem
      fi
      TAG=$(extract_tags "$release_file" | sed -n '1p' || true)
      ;;
    rc|dev|staging)
      list_file="$TMP_ROOT/releases.json"
      if ! http_get "$GITHUB_API/repos/$REPOSITORY/releases?per_page=100" "$list_file"; then
        api_problem
      fi
      TAG=""
      while IFS= read -r candidate; do
        if [[ "$CHANNEL" == "rc" && "$candidate" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-rc\.[1-9][0-9]*$ ]]; then
          TAG="$candidate"
        elif [[ "$CHANNEL" == "dev" && "$candidate" == dev-* ]]; then
          TAG="$candidate"
        elif [[ "$CHANNEL" == "staging" && "$candidate" == staging-* ]]; then
          TAG="$candidate"
        fi
        [[ -z "$TAG" ]] || break
      done < <(extract_tags "$list_file")
      if [[ -z "$TAG" ]]; then
        fail "no $CHANNEL build has been published yet"
      fi
      if ! http_get "$GITHUB_API/repos/$REPOSITORY/releases/tags/$TAG" "$release_file"; then
        api_problem
      fi
      ;;
  esac
fi

if [[ -z "${TAG:-}" ]]; then
  api_problem
fi

if [[ "$TAG" =~ ^dev-[0-9]{8}-[0-9a-f]{12}$ ]]; then
  DISPLAY_CHANNEL="dev"
elif [[ "$TAG" =~ ^staging-[0-9]{8}-[0-9a-f]{12}$ ]]; then
  DISPLAY_CHANNEL="staging"
elif [[ "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-rc\.[1-9][0-9]*$ ]]; then
  DISPLAY_CHANNEL="rc"
elif [[ "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
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
ASSET="aforge-${OS}-${ARCH}${extension}"

asset_id() {
  local name="$1"
  # Release asset names are ASCII strings created by this repository's own
  # workflow, so this scanner deliberately does not decode JSON \u escapes.
  awk -v wanted="$name" '
    { document = document $0 "\n" }
    END {
      depth = 0
      for (position = 1; position <= length(document); position++) {
        character = substr(document, position, 1)
        if (character == "{") {
          parent = depth
          candidate = parent > 0 && kinds[parent] == "array" && asset_arrays[parent]
          if (parent > 0 && kinds[parent] == "object" && expects_value[parent]) {
            expects_value[parent] = 0
          }
          depth++
          kinds[depth] = "object"
          asset_objects[depth] = candidate
          names[depth] = ""
          ids[depth] = ""
          keys[depth] = ""
          expects_value[depth] = 0
          continue
        }
        if (character == "}") {
          if (asset_objects[depth] && names[depth] == wanted && ids[depth] != "") {
            print ids[depth]
            exit
          }
          delete kinds[depth]
          delete asset_objects[depth]
          delete names[depth]
          delete ids[depth]
          delete keys[depth]
          delete expects_value[depth]
          depth--
          continue
        }
        if (character == "[") {
          parent = depth
          assets = parent == 1 && kinds[parent] == "object" && expects_value[parent] && keys[parent] == "assets"
          if (parent > 0 && kinds[parent] == "object" && expects_value[parent]) {
            expects_value[parent] = 0
          }
          depth++
          kinds[depth] = "array"
          asset_arrays[depth] = assets
          continue
        }
        if (character == "]") {
          delete kinds[depth]
          delete asset_arrays[depth]
          depth--
          continue
        }
        if (character == "\"") {
          token = ""
          escaped = 0
          for (position++; position <= length(document); position++) {
            character = substr(document, position, 1)
            if (escaped) {
              token = token character
              escaped = 0
            } else if (character == "\\") {
              token = token character
              escaped = 1
            } else if (character == "\"") {
              break
            } else {
              token = token character
            }
          }
          after = position + 1
          while (substr(document, after, 1) ~ /[[:space:]]/) {
            after++
          }
          if (kinds[depth] == "object" && substr(document, after, 1) == ":") {
            keys[depth] = token
            expects_value[depth] = 1
          } else if (kinds[depth] == "object" && expects_value[depth]) {
            if (asset_objects[depth] && keys[depth] == "name") {
              names[depth] = token
            }
            expects_value[depth] = 0
          }
          continue
        }
        if (kinds[depth] == "object" && expects_value[depth] && character ~ /[0-9]/) {
          value = character
          while (substr(document, position + 1, 1) ~ /[0-9]/) {
            position++
            value = value substr(document, position, 1)
          }
          if (asset_objects[depth] && keys[depth] == "id") {
            ids[depth] = value
          }
          expects_value[depth] = 0
          continue
        }
        if (character == "," && kinds[depth] == "object") {
          keys[depth] = ""
          expects_value[depth] = 0
        }
      }
    }
  ' "$release_file"
}

download_asset() {
  local name="$1"
  local destination="$2"
  if [[ -n "$TOKEN" ]]; then
    local id
    id=$(asset_id "$name")
    [[ -n "$id" ]] || fail "release $TAG has no $name asset"
    if ! http_get "$GITHUB_API/repos/$REPOSITORY/releases/assets/$id" "$destination" "application/octet-stream"; then
      api_problem
    fi
  else
    if ! http_get "$GITHUB_DOWNLOAD/$REPOSITORY/releases/download/$TAG/$name" "$destination" "application/octet-stream"; then
      fail "could not download $name; export GITHUB_TOKEN if the repository is private"
    fi
  fi
}

if [[ -n "$DISPLAY_CHANNEL" ]]; then
  printf 'aforge: %s %s for %s/%s\n' "$DISPLAY_CHANNEL" "$TAG" "$OS" "$ARCH"
else
  printf 'aforge: %s for %s/%s\n' "$TAG" "$OS" "$ARCH"
fi
download_asset "$ASSET" "$TMP_ROOT/$ASSET"
download_asset "checksums.txt" "$TMP_ROOT/checksums.txt"

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

mkdir -p "$INSTALL_DIR"
INSTALL_TEMP="$INSTALL_DIR/.aforge.tmp.$$"
cp "$TMP_ROOT/$ASSET" "$INSTALL_TEMP"
chmod 0755 "$INSTALL_TEMP"
mv -f "$INSTALL_TEMP" "$INSTALL_DIR/aforge${extension}"
INSTALL_TEMP=""
printf 'aforge: installed %s\n' "$INSTALL_DIR/aforge${extension}"

path_has_dir() {
  case ":${PATH}:" in
    *":$INSTALL_DIR:"*) return 0 ;;
    *) return 1 ;;
  esac
}

append_path_line() {
  local file="$1"
  local line="$2"
  mkdir -p "$(dirname "$file")"
  if [[ ! -f "$file" ]] || ! grep -F '# aforge installer' "$file" >/dev/null 2>&1; then
    printf '%s\n' "$line" >> "$file"
  fi
}

if [[ "$OS" != "windows" ]] && ! path_has_dir; then
  export_line="export PATH=\"$INSTALL_DIR:\$PATH\""
  printf 'aforge: add it to this shell with: %s\n' "$export_line"
  if [[ "$NO_MODIFY_PATH" != "1" ]]; then
    shell_name=$(basename "${SHELL:-/bin/bash}")
    case "$shell_name" in
      zsh)
        append_path_line "$HOME/.zshrc" "$export_line # aforge installer"
        ;;
      fish)
        append_path_line "$HOME/.config/fish/config.fish" "fish_add_path \"$INSTALL_DIR\" # aforge installer"
        ;;
      *)
        append_path_line "$HOME/.bashrc" "$export_line # aforge installer"
        if [[ "$OS" == "darwin" && -f "$HOME/.bash_profile" ]]; then
          append_path_line "$HOME/.bash_profile" "$export_line # aforge installer"
        fi
        ;;
    esac
  fi
fi

"$INSTALL_DIR/aforge${extension}" version

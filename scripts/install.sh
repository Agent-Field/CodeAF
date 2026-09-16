#!/usr/bin/env bash

set -euo pipefail

REPOSITORY="Agent-Field/codeaf"
LEGACY_REPOSITORY="Agent-Field/aforge-v2" # Remove after the one-release repository fallback. # legacy-name
CHANNEL="${CHANNEL:-stable}"
VERSION="${VERSION:-}"
VERBOSE="${VERBOSE:-0}"
NO_MODIFY_PATH="${CODEAF_NO_MODIFY_PATH:-${AFORGE_NO_MODIFY_PATH:-0}}" # legacy-name
INSTALL_DIR="${CODEAF_INSTALL_DIR:-${AFORGE_INSTALL_DIR:-${HOME}/.codeaf/bin}}" # legacy-name
STATE_ROOT="${CODEAF_HOME:-${AFORGE_HOME:-${HOME}/.codeaf}}" # legacy-name
GITHUB_API="${CODEAF_GITHUB_API:-${AFORGE_GITHUB_API:-https://api.github.com}}" # legacy-name
GITHUB_DOWNLOAD="${CODEAF_GITHUB_DOWNLOAD:-${AFORGE_GITHUB_DOWNLOAD:-https://github.com}}" # legacy-name
# GitHub answers anonymous API calls sixty times an hour per address; a token raises that.
TOKEN="${GITHUB_TOKEN:-${GH_TOKEN:-}}"

usage() {
  cat <<'EOF'
Install codeaf from a GitHub release.

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
  --dir PATH          Install somewhere other than ~/.codeaf/bin.
  --no-modify-path    Print the PATH line without editing a shell file.
  --verbose           Print download details.
  --help              Show this help.

Environment:
  CHANNEL, VERSION, CODEAF_INSTALL_DIR, CODEAF_NO_MODIFY_PATH, VERBOSE
  GITHUB_TOKEN or GH_TOKEN: GitHub answers anonymous API calls sixty times an hour per address; a token raises that.
  CODEAF_GITHUB_API and CODEAF_GITHUB_DOWNLOAD for mirrors and tests
EOF
}

fail() {
  printf 'codeaf: %s\n' "$*" >&2
  exit 1
}

usage_error() {
  printf 'codeaf: %s\n\n' "$*" >&2
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

TMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/codeaf.XXXXXX")
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
  local authenticate="${4:-0}"
  local status
  if [[ "$VERBOSE" == "1" ]]; then
    printf 'codeaf: GET %s\n' "$url"
  fi
  if command -v curl >/dev/null 2>&1; then
    local args=(-sSL --output "$destination" --write-out '%{http_code}' -H "Accept: ${accept}")
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

  local headers="$TMP_ROOT/http.headers"
  local wget_code
  local args=(-O "$destination" -S --header="Accept: ${accept}")
  if [[ "$authenticate" == "1" && -n "$TOKEN" ]]; then
    args+=(--header="Authorization: Bearer ${TOKEN}")
  fi
  if wget "${args[@]}" "$url" 2> "$headers"; then
    wget_code=0
  else
    wget_code=$?
  fi
  status=$(awk '$1 ~ /^HTTP\/[0-9.]+$/ && $2 ~ /^[0-9][0-9][0-9]$/ {status=$2} END {print status}' "$headers")
  HTTP_STATUS="$status"
  if [[ "$wget_code" == "0" && "$status" == 2* ]]; then
    return 0
  fi
  return 1
}

api_problem() {
  fail "GitHub's API could not be reached or refused (a rate limit, or a repository you cannot read?); pin VERSION=<tag>, or export GITHUB_TOKEN"
}

release_api_get() {
  local suffix="$1"
  local destination="$2"
  if http_get "$GITHUB_API/repos/$REPOSITORY/$suffix" "$destination" "application/vnd.github+json" 1; then
    return 0
  fi
  if [[ "$HTTP_STATUS" == "404" && "$REPOSITORY" != "$LEGACY_REPOSITORY" ]]; then
    # Remove after the renamed repository has carried releases for one release.
    if http_get "$GITHUB_API/repos/$LEGACY_REPOSITORY/$suffix" "$destination" "application/vnd.github+json" 1; then
      REPOSITORY="$LEGACY_REPOSITORY"
      return 0
    fi
  fi
  return 1
}

extract_tags() {
  grep -Eo '"tag_name"[[:space:]]*:[[:space:]]*"[^"]+"' "$1" |
    sed -E 's/^"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)"$/\1/'
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

release_file="$TMP_ROOT/release.json"
if [[ -n "$VERSION" ]]; then
  TAG="$VERSION"
else
  case "$CHANNEL" in
    stable)
      if ! release_api_get "releases/latest" "$release_file"; then
        if [[ "$HTTP_STATUS" == "404" ]]; then
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
        if [[ "$CHANNEL" == "rc" && "$candidate" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-rc\.[1-9][0-9]*$ ]] ||
          [[ "$CHANNEL" == "dev" && "$candidate" == dev-* ]] ||
          [[ "$CHANNEL" == "staging" && "$candidate" == staging-* ]]; then
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

if [[ -n "$DISPLAY_CHANNEL" ]]; then
	printf 'codeaf: %s %s for %s/%s\n' "$DISPLAY_CHANNEL" "$TAG" "$OS" "$ARCH"
else
	printf 'codeaf: %s for %s/%s\n' "$TAG" "$OS" "$ARCH"
fi
DOWNLOAD_REPOSITORY="$REPOSITORY"
if ! download_release "$DOWNLOAD_REPOSITORY"; then
	if [[ "$HTTP_STATUS" == "404" && "$DOWNLOAD_REPOSITORY" != "$LEGACY_REPOSITORY" ]]; then
		# Remove after the renamed repository has carried releases for one release.
		DOWNLOAD_REPOSITORY="$LEGACY_REPOSITORY"
		if ! download_release "$DOWNLOAD_REPOSITORY"; then
			fail "could not download codeaf-${OS}-${ARCH}${extension}; check the tag on the Releases page"
		fi
	else
		fail "could not download codeaf-${OS}-${ARCH}${extension}; check the tag on the Releases page"
	fi
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
INSTALL_TEMP="$INSTALL_DIR/.codeaf.tmp.$$"
cp "$TMP_ROOT/$ASSET" "$INSTALL_TEMP"
chmod 0755 "$INSTALL_TEMP"
mv -f "$INSTALL_TEMP" "$INSTALL_DIR/codeaf${extension}"
INSTALL_TEMP=""
printf 'codeaf: installed %s\n' "$INSTALL_DIR/codeaf${extension}"

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

if [[ "$OS" != "windows" ]] && ! path_has_dir; then
  export_line="export PATH=\"$INSTALL_DIR:\$PATH\""
  printf 'codeaf: add it to this shell with: %s\n' "$export_line"
  if [[ "$NO_MODIFY_PATH" != "1" ]]; then
    shell_name=$(basename "${SHELL:-/bin/bash}")
    case "$shell_name" in
      zsh)
        append_path_line "$HOME/.zshrc" "$export_line # codeaf installer"
        ;;
      fish)
        append_path_line "$HOME/.config/fish/config.fish" "fish_add_path \"$INSTALL_DIR\" # codeaf installer"
        ;;
      *)
        append_path_line "$HOME/.bashrc" "$export_line # codeaf installer"
        if [[ "$OS" == "darwin" && -f "$HOME/.bash_profile" ]]; then
          append_path_line "$HOME/.bash_profile" "$export_line # codeaf installer"
        fi
        ;;
    esac
  fi
fi

if [[ "$RUN_BOOT_ADOPTION" == "1" ]]; then
  "$INSTALL_DIR/codeaf${extension}" version
else
  CODEAF_HOME="$STATE_ROOT" "$INSTALL_DIR/codeaf${extension}" version
fi

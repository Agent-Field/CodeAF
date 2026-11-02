#!/usr/bin/env bash
# Print one deterministic sha256 over the hosted relay's inputs (paths and
# contents, in sorted order):
#   - relay/hosted/** (tracked source inputs, excluding node_modules, .wrangler
#     and generated build products)
#   - scripts/raft-hosted-build.sh
#   - scripts/raft-proof-footprint.sh
#   - go.mod and go.sum
#   - the Go source of every package in
#     `GOOS=wasip1 GOARCH=wasm go list -deps ./cmd/codeaf-raft-wasm`
#     that lives in this module
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
root="$(cd "${RELAY_FINGERPRINT_ROOT:-$repo_root}" && pwd)"

list_file="$(mktemp "${TMPDIR:-/tmp}/relay-fingerprint.XXXXXX")"
trap 'rm -f "$list_file"' EXIT

(
	cd "$root"

	find relay/hosted \
		\( -path 'relay/hosted/node_modules' -o -path 'relay/hosted/.wrangler' \) -prune \
		-o -type f \
		! -path 'relay/hosted/src/raft/core.wasm' \
		! -path 'relay/hosted/wrangler.ci.toml' \
		-print

	for fixed in \
		scripts/raft-hosted-build.sh \
		scripts/raft-proof-footprint.sh \
		go.mod \
		go.sum; do
		if [ ! -f "$fixed" ]; then
			echo "missing relay input: $fixed" >&2
			exit 1
		fi
		printf '%s\n' "$fixed"
	done

	GOOS=wasip1 GOARCH=wasm go list -buildvcs=false -deps \
		-f '{{if .Module}}{{range .GoFiles}}{{$.Dir}}/{{.}}
{{end}}{{range .CgoFiles}}{{$.Dir}}/{{.}}
{{end}}{{range .SFiles}}{{$.Dir}}/{{.}}
{{end}}{{range .HFiles}}{{$.Dir}}/{{.}}
{{end}}{{range .EmbedFiles}}{{$.Dir}}/{{.}}
{{end}}{{end}}' \
		./cmd/codeaf-raft-wasm | while IFS= read -r abs_path; do
		[ -n "$abs_path" ] || continue
		case "$abs_path" in
			"$root"/*)
				printf '%s\n' "${abs_path#"$root"/}"
				;;
		esac
	done
) | LC_ALL=C sort -u > "$list_file"

if [ ! -s "$list_file" ]; then
	echo "relay-fingerprint found no input files" >&2
	exit 1
fi

(
	cd "$root"
	tr '\n' '\0' < "$list_file" | xargs -0 sha256sum -b --
) | sha256sum | awk '{print $1}'

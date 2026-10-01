#!/usr/bin/env bash
# ONE WALK FOR THE LAPTOP AND CI. Only surviving directories in the root
# module are targets; a fixture with its own go.mod belongs to another tree.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

base="${BASE:-origin/dev}"
case "$base" in '' | 0000000000000000000000000000000000000000) base=HEAD~1 ;; esac
git rev-parse --verify "$base^{commit}" >/dev/null
changed="$(git diff --name-only "$base" HEAD -- '*.go' go.mod go.sum)"
if printf '%s\n' "$changed" | grep -qxE 'go\.(mod|sum)'; then
	# go list excludes nested modules and returns concrete packages, so the same
	# list can be partitioned without a second interpretation of ./....
	go list -f '{{if .GoFiles}}{{.Dir}}{{else if .CgoFiles}}{{.Dir}}{{else if .TestGoFiles}}{{.Dir}}{{else if .XTestGoFiles}}{{.Dir}}{{end}}' ./... |
		while IFS= read -r dir; do
			[ -n "$dir" ] || continue
			if [ "$dir" = "$PWD" ]; then printf './\n'; else printf './%s\n' "${dir#"$PWD"/}"; fi
		done | LC_ALL=C sort -u
else
	printf '%s\n' "$changed" | while IFS= read -r file; do
		case "$file" in *.go) dirname "$file" ;; esac
	done | LC_ALL=C sort -u | while IFS= read -r dir; do
		[ -n "$dir" ] || continue
		nested=; walk="$dir"
		while [ "$walk" != . ] && [ "$walk" != / ]; do
			if [ -f "$walk/go.mod" ]; then nested=yes; break; fi
			walk="$(dirname "$walk")"
		done
		[ -z "$nested" ] || continue
		# Deleting the final source file deletes the target, even if an asset or
		# an empty directory remains in the checkout.
		if compgen -G "$dir/*.go" >/dev/null; then
			if [ "$dir" = . ]; then printf './\n'; else printf './%s\n' "$dir"; fi
		fi
	done
fi

#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
fp_script="$repo_root/scripts/relay-fingerprint.sh"

tmp="$(mktemp -d "${TMPDIR:-/tmp}/relay-fingerprint-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

fail() {
	echo "FAIL: $*" >&2
	exit 1
}

# Build a lightweight sandbox mirror of the module so mutations never touch the real tree.
sandbox="$tmp/repo"
mkdir -p "$sandbox/scripts" "$sandbox/relay" "$sandbox/cmd" "$sandbox/internal/tui3"
cp "$repo_root/go.mod" "$repo_root/go.sum" "$sandbox/"
cp "$repo_root/scripts/raft-hosted-build.sh" "$repo_root/scripts/raft-proof-footprint.sh" "$sandbox/scripts/"
cp -R "$repo_root/cmd/codeaf-raft-wasm" "$sandbox/cmd/"

# Copy relay/hosted without heavy node_modules/.wrangler.
mkdir -p "$sandbox/relay/hosted"
(
	cd "$repo_root/relay/hosted"
	find . \( -path './node_modules' -o -path './.wrangler' \) -prune -o -type f -print | while IFS= read -r rel; do
		mkdir -p "$sandbox/relay/hosted/$(dirname "$rel")"
		cp "$rel" "$sandbox/relay/hosted/$rel"
	done
)

# Copy every in-tree wasm dependency package (including replaced third_party/raft) plus one unrelated package.
(
	cd "$repo_root"
	GOOS=wasip1 GOARCH=wasm go list -buildvcs=false -deps \
		-f '{{if .Module}}{{.Dir}}{{end}}' \
		./cmd/codeaf-raft-wasm
) | sort -u | while IFS= read -r pkg_dir; do
	[ -n "$pkg_dir" ] || continue
	case "$pkg_dir" in
		"$repo_root"/*)
			rel_dir="${pkg_dir#"$repo_root"/}"
			mkdir -p "$sandbox/$(dirname "$rel_dir")"
			rm -rf "$sandbox/$rel_dir"
			cp -R "$pkg_dir" "$sandbox/$rel_dir"
			;;
	esac
done
if [ -f "$repo_root/third_party/raft/go.mod" ]; then
	mkdir -p "$sandbox/third_party/raft"
	cp "$repo_root/third_party/raft/go.mod" "$sandbox/third_party/raft/go.mod"
	if [ -f "$repo_root/third_party/raft/go.sum" ]; then
		cp "$repo_root/third_party/raft/go.sum" "$sandbox/third_party/raft/go.sum"
	fi
fi

cat > "$sandbox/internal/tui3/unrelated.go" <<'EOF'
package tui3

const Unrelated = 1
EOF

base="$(RELAY_FINGERPRINT_ROOT="$sandbox" bash "$fp_script")"
[[ "$base" =~ ^[0-9a-f]{64}$ ]] || fail "fingerprint is not 64 lowercase hex chars: $base"

again="$(RELAY_FINGERPRINT_ROOT="$sandbox" bash "$fp_script")"
[ "$base" = "$again" ] || fail "fingerprint is not deterministic: $base vs $again"

# Generated/ignored files under relay/hosted must not change the fingerprint.
mkdir -p "$sandbox/relay/hosted/node_modules/fake" "$sandbox/relay/hosted/.wrangler/state" "$sandbox/relay/hosted/src/raft"
echo "ignored" > "$sandbox/relay/hosted/node_modules/fake/index.js"
echo "ignored" > "$sandbox/relay/hosted/.wrangler/state/cache"
echo "ignored" > "$sandbox/relay/hosted/src/raft/core.wasm"
echo "ignored" > "$sandbox/relay/hosted/wrangler.ci.toml"
with_ignored="$(RELAY_FINGERPRINT_ROOT="$sandbox" bash "$fp_script")"
[ "$base" = "$with_ignored" ] || fail "ignored artifacts changed fingerprint: $base vs $with_ignored"

# Unrelated Go packages and test files must not change the fingerprint.
echo "// comment" >> "$sandbox/internal/tui3/unrelated.go"
echo "// test edit" >> "$sandbox/internal/raftcore/core_test.go"
after_unrelated="$(RELAY_FINGERPRINT_ROOT="$sandbox" bash "$fp_script")"
[ "$base" = "$after_unrelated" ] || fail "unrelated package or _test.go changed fingerprint: $base vs $after_unrelated"

# Every declared input category must change the fingerprint when its content or path changes.
check_mutation_changes() {
	local label="$1" target="$2" trailer="$3"
	cp "$target" "$tmp/saved"
	printf '\n%s\n' "$trailer" >> "$target"
	local mutated
	mutated="$(RELAY_FINGERPRINT_ROOT="$sandbox" bash "$fp_script")"
	mv "$tmp/saved" "$target"
	[ "$mutated" != "$base" ] || fail "editing $label did not change fingerprint"
	local restored
	restored="$(RELAY_FINGERPRINT_ROOT="$sandbox" bash "$fp_script")"
	[ "$restored" = "$base" ] || fail "restoring $label did not restore fingerprint"
}

check_mutation_changes "relay/hosted JS" "$sandbox/relay/hosted/src/worker.js" "// change"
check_mutation_changes "raft-hosted-build.sh" "$sandbox/scripts/raft-hosted-build.sh" "# change"
check_mutation_changes "raft-proof-footprint.sh" "$sandbox/scripts/raft-proof-footprint.sh" "# change"
check_mutation_changes "go.mod" "$sandbox/go.mod" "// change"
check_mutation_changes "go.sum" "$sandbox/go.sum" ""

# Mutating a Go source file in a wasm-dep package must change the fingerprint.
cp "$sandbox/internal/raftcore/core.go" "$tmp/core.go.bak"
printf '\n// relay fingerprint probe\n' >> "$sandbox/internal/raftcore/core.go"
wasm_mutated="$(RELAY_FINGERPRINT_ROOT="$sandbox" bash "$fp_script")"
mv "$tmp/core.go.bak" "$sandbox/internal/raftcore/core.go"
[ "$wasm_mutated" != "$base" ] || fail "editing wasm dependency Go source did not change fingerprint"

# Renaming a file while keeping identical contents must change the fingerprint (paths are hashed).
mv "$sandbox/relay/hosted/src/base.js" "$sandbox/relay/hosted/src/base-renamed.js"
renamed_fp="$(RELAY_FINGERPRINT_ROOT="$sandbox" bash "$fp_script")"
mv "$sandbox/relay/hosted/src/base-renamed.js" "$sandbox/relay/hosted/src/base.js"
[ "$renamed_fp" != "$base" ] || fail "renaming a file with identical bytes did not change fingerprint"

echo "PASS: relay-fingerprint_test"

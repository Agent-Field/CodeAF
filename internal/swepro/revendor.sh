#!/usr/bin/env bash
# The initial import of swe-pro-go into aforge, scripted so the starting point
# is reproducible.
#
#   internal/swepro/revendor.sh <path-to-swe-pro-go-checkout> [ref]
#
# This ran once, at af248e9 (see UPSTREAM). It exists so anyone can re-derive
# that starting point from the upstream checkout and see that the import was
# mechanical — a copy, three substitutions, two dropped binaries, nothing
# clever.
#
# IT IS NOT A RE-SYNC TOOL. From the moment the import landed, internal/swepro
# is aforge code: modified freely, in place, by normal commits
# (docs/SUBHARNESSES.md, "The engine copy is owned, not borrowed"). Running
# this against a newer upstream ref would overwrite every one of those edits.
# Harvesting an upstream improvement is a cherry-pick against the divergence
# log in EMBEDDING.md, not a re-run of this script.
set -euo pipefail

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
	echo "usage: $0 <path-to-swe-pro-go-checkout> [ref]" >&2
	exit 2
fi

UPSTREAM_DIR=$(cd "$1" && pwd)
REF=${2:-HEAD}
HERE=$(cd "$(dirname "$0")" && pwd)

OLD_MODULE="github.com/Agent-Field/swe-pro-go"
NEW_PACKAGE="github.com/Agent-Field/aforge-v2/internal/swepro"

if [ ! -d "$UPSTREAM_DIR/.git" ]; then
	echo "$UPSTREAM_DIR is not a git checkout" >&2
	exit 1
fi
if ! git -C "$UPSTREAM_DIR" rev-parse --verify --quiet "$REF^{commit}" >/dev/null; then
	echo "$REF is not a commit in $UPSTREAM_DIR" >&2
	exit 1
fi

COMMIT=$(git -C "$UPSTREAM_DIR" rev-parse "$REF")
SHORT=$(git -C "$UPSTREAM_DIR" rev-parse --short "$REF")
SUBJECT=$(git -C "$UPSTREAM_DIR" log -1 --format=%s "$REF")

# Export the ref rather than the working tree, so whatever branch happens to
# be checked out upstream cannot leak into the result.
STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT
git -C "$UPSTREAM_DIR" archive --format=tar "$REF" | tar -x -C "$STAGE"

echo "import: $OLD_MODULE @ $SHORT ($SUBJECT)"

# ---------------------------------------------------------------- copy ------
# The engine's internal tree keeps its name, so Go's internal rule scopes all
# 121 of its packages to the internal/swepro subtree. cmd/codeaf becomes the
# codeaf package: the one door. cmd/swedog (a standalone watchdog binary) and
# cmd/plandb-diff (a developer diff tool) are dropped — nothing on the
# subharness path calls either. cmd/codeaf/serve.go is deliberately KEPT: it
# compiles against aforge's newer agentfield SDK unchanged.
rm -rf "$HERE/internal" "$HERE/codeaf"
mkdir -p "$HERE/internal" "$HERE/codeaf"
rsync -a --delete "$STAGE/internal/" "$HERE/internal/"
rsync -a --delete "$STAGE/cmd/codeaf/" "$HERE/codeaf/"

# The three upstream documents a maintainer of this tree needs. The rest
# (AGENTFIELD.md, DOGFOOD.md, FIX-CANDIDATES.md, OBSERVER-WIRING.md) stay
# upstream. EVENTS-CONTRACT.md travels with the code that emits it, because
# v1 of the subharness reads exactly that stream.
for doc in BUGS-KEPT.md ENGINE-DESIGN.md EVENTS-CONTRACT.md; do
	cp "$STAGE/$doc" "$HERE/$doc"
done

# ------------------------------------------------------------- rewrite ------
# 1. Every import of the upstream module's internal tree becomes the same
#    package under ours. The package layout below the module path is
#    identical, which is why one substitution is the whole rewrite.
grep -rl "$OLD_MODULE/internal/" "$HERE/internal" "$HERE/codeaf" --include '*.go' |
	xargs sed -i.bak "s|$OLD_MODULE/internal/|$NEW_PACKAGE/internal/|g"

# 2. cmd/codeaf was `package main`; it is now a library package named for the
#    command it used to be. The clause is anchored, so a `package main` inside
#    a string literal or a testdata fixture is left alone.
sed -i.bak 's|^package main$|package codeaf|' "$HERE"/codeaf/*.go

# 3. One directory shallower. Upstream this package was cmd/codeaf, two levels
#    under the repo root, so its fixtures reached the engine's tree as
#    ../../internal/. Here it is internal/swepro/codeaf, and that path is
#    ../internal/.
sed -i.bak 's|"\.\./\.\./internal/|"../internal/|g' "$HERE"/codeaf/*.go

find "$HERE/internal" "$HERE/codeaf" -name '*.bak' -delete

# String literals naming swe-pro-go (default control-plane node IDs, test
# fixtures) are data, not imports, and are deliberately left alone: changing
# them would be a behavior change, and this script only moves code.
if grep -rq "$OLD_MODULE/" "$HERE/internal" "$HERE/codeaf" --include '*.go'; then
	echo "import: an import of $OLD_MODULE survived the rewrite" >&2
	grep -rn "$OLD_MODULE/" "$HERE/internal" "$HERE/codeaf" --include '*.go' >&2
	exit 1
fi

# -------------------------------------------------------------- stamp -------
# The import point, for the divergence log to be read against.
cat >"$HERE/UPSTREAM" <<STAMP
repo    $OLD_MODULE
commit  $COMMIT
short   $SHORT
subject $SUBJECT
ref     $REF
STAMP

echo "import: done — internal/swepro is $OLD_MODULE @ $SHORT, mechanically rewritten"
echo "import: the embedding edits on top of it are listed in EMBEDDING.md's divergence log"

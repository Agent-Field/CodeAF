#!/usr/bin/env bash
# toolkit — the near-empty Go module the bundle3 cell builds into.
#
# The cell's whole point is the *shape* of the ask: three packages that share a
# module and nothing else. So the fixture is deliberately almost nothing — a
# module path, a go directive old enough not to pull a toolchain, and a doc file
# that names the three packages without writing any of them. Anything more would
# be the harness doing the work the cell is measuring.
#
# Usage: toolkit.sh <dir>
set -euo pipefail

DIR="${1:?usage: toolkit.sh <dir>}"

rm -rf "$DIR"
mkdir -p "$DIR"

cat > "$DIR/go.mod" <<'EOF'
module toolkit

go 1.21
EOF

cat > "$DIR/doc.go" <<'EOF'
// Package toolkit is a home for small, independent helper packages.
//
// Each package under this module stands alone: no package here imports another,
// and each carries its own tests. Nothing is implemented yet.
package toolkit
EOF

echo "$DIR"

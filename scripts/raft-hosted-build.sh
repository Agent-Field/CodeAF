#!/bin/sh
# Wrangler invokes this from relay/hosted. The generated reactor is bundled
# through a static import and is never an independently deployed service.
#
# AN UNCHANGED BUILD MUST NOT TOUCH core.wasm. Wrangler watches src/ and runs
# this hook again on any change there, so rewriting the file with the same
# bytes re-triggered the build, which rewrote it again: under disk load the
# local dev server rebuilt and reloaded in a loop, and every reload of this
# eleven-megabyte module grew the Node process until it aborted at about nine
# gigabytes. The Go build is reproducible, so the reactor is built beside the
# tree and moved into place only when its bytes differ.
set -eu
cd ../..
out=relay/hosted/src/raft/core.wasm
built=$(mktemp "${TMPDIR:-/tmp}/codeaf-core-wasm.XXXXXX")
trap 'rm -f "$built"' EXIT
sh scripts/raft-proof-footprint.sh env GOOS=wasip1 GOARCH=wasm GOMEMLIMIT=192MiB go build -p=1 -trimpath -buildmode=c-shared -ldflags='-s -w' -o "$built" ./cmd/codeaf-raft-wasm
if ! cmp -s "$built" "$out"; then
  cp "$built" "$out.tmp"
  mv -f "$out.tmp" "$out"
fi

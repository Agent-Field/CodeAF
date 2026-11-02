#!/bin/sh
# Wrangler invokes this from relay/hosted. The generated reactor is bundled
# through a static import and is never an independently deployed service.
set -eu
cd ../..
sh scripts/raft-proof-footprint.sh env GOOS=wasip1 GOARCH=wasm GOMEMLIMIT=192MiB go build -p=1 -trimpath -buildmode=c-shared -ldflags='-s -w' -o relay/hosted/src/raft/core.wasm ./cmd/codeaf-raft-wasm

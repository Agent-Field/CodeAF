#!/bin/sh
# This feasibility artifact is a WASI reactor and a stdio conformance peer,
# never an alternate installation of the person's codeaf binary.
set -eu
out=${1:?usage: raft-proof-build.sh /tmp/output-directory}
mkdir -p "$out"
sh scripts/raft-proof-footprint.sh env GOMEMLIMIT=192MiB GOOS=wasip1 GOARCH=wasm go build -p=1 -trimpath -buildmode=c-shared -ldflags='-s -w' -o "$out/core.wasm" ./cmd/codeaf-raft-wasm
sh scripts/raft-proof-footprint.sh env GOMEMLIMIT=192MiB go build -p=1 -trimpath -ldflags='-s -w' -o "$out/native-peer" ./cmd/codeaf-raft-probe

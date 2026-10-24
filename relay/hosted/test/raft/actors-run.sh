#!/bin/sh
# All phases are serial, pressure-admitted and local. A watchdog bounds each
# runtime phase without weakening resource or correctness assertions.
set -eu
out=${1:-/tmp/codeaf-p08}
sh scripts/raft-proof-build.sh "$out"
sh scripts/raft-proof-footprint.sh go test -p=1 ./internal/raftabi ./internal/raftcore ./internal/raftsnapshot -run '^TestR' -count=1 -timeout=120s
cd relay/hosted
sh ../../scripts/raft-proof-footprint.sh sh test/local.sh env CODEAF_RAFT_ACTORS=1 node test/raft/build.mjs "$out"
sh ../../scripts/raft-proof-footprint.sh sh test/local.sh env CODEAF_RAFT_PROOF_DIR="$out" node --max-old-space-size=32 test/raft/actor-unit.test.js
sh ../../scripts/raft-proof-footprint.sh sh test/local.sh env CODEAF_RAFT_PROOF_DIR="$out" timeout 120s node --max-old-space-size=24 test/raft/actors.test.js

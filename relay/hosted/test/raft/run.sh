#!/bin/sh
# Run from the repository root. No cloud credentials, user home or hosted
# endpoint participates; all storage and build artifacts are disposable.
set -eu
out=${1:-/tmp/codeaf-p03}
sh scripts/raft-proof-build.sh "$out"
sh scripts/raft-proof-footprint.sh go test -p=1 ./internal/raftabi ./internal/raftcore -run '^TestR' -count=1
sh scripts/raft-proof-footprint.sh sh -c 'cd relay/hosted && sh test/local.sh node test/raft/build.mjs "$1"' proof "$out"
sh scripts/raft-proof-footprint.sh sh -c 'cd relay/hosted && sh test/local.sh timeout --signal=TERM --kill-after=5s 120s node --max-old-space-size=24 test/raft/abi.test.js'
sh scripts/raft-proof-footprint.sh sh -c 'cd relay/hosted && CODEAF_RAFT_PROOF_DIR="$1" sh test/local.sh timeout --signal=TERM --kill-after=5s 120s node --max-old-space-size=24 test/raft/smoke.test.js' proof "$out"
read -r workerd_binary < "$out/workerd-path"
read -r workerd_schema < "$out/workerd-schema-path"
sh scripts/raft-proof-footprint.sh timeout --signal=TERM --kill-after=5s 120s env GOMEMLIMIT=32MiB GOGC=50 "$out/native-peer" measure "$out" "$workerd_binary" "$workerd_schema"

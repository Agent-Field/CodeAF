#!/bin/sh
# Pressure admission precedes every heavy step to bound resource contention.
#
# ONLY LINUX REPORTS PRESSURE. macOS and a Linux kernel built without PSI have
# no /proc/pressure/io, and waiting on a file that cannot exist failed the
# hosted relay's build hook outright there, so `npm run dev` never started on a
# Mac. Where the file is missing there is nothing to wait for and the step runs
# at once; where it exists the wait is exactly what it was.
set -eu
psi=${CODEAF_RAFT_PROOF_PSI:-/proc/pressure/io}
if [ -r "$psi" ]; then
  while ! awk '/^some / {split($2,a,"="); exit !(a[2]<=40)}' "$psi"; do
    cat "$psi"
    sleep 60
  done
  cat "$psi"
fi
export GOFLAGS=-buildvcs=false GOMAXPROCS=2
# Bound allocator arenas in the local Node/workerd hosts on many-core WSL.
export MALLOC_ARENA_MAX=1 MALLOC_TRIM_THRESHOLD_=65536
exec nice -n 10 "$@"

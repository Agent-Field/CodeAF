#!/bin/sh
# The shared laptop is in use; pressure admission precedes every heavy step.
set -eu
while ! awk '/^some / {split($2,a,"="); exit !(a[2]<=40)}' /proc/pressure/io; do
  cat /proc/pressure/io
  sleep 60
done
cat /proc/pressure/io
export GOFLAGS=-buildvcs=false GOMAXPROCS=2
# Bound allocator arenas in the local Node/workerd hosts on many-core WSL.
export MALLOC_ARENA_MAX=1 MALLOC_TRIM_THRESHOLD_=65536
exec nice -n 10 "$@"

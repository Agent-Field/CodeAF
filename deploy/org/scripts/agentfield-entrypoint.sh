#!/usr/local/bin/busybox sh
# Entrypoint of the control-plane image. The published image is distroless and runs as uid 65532,
# but a platform volume (Railway's) arrives owned by root, and the control plane has to write its
# database there. So this starts as root, hands the data folder to 65532 once, and runs the control
# plane as 65532 (see scripts/drop-root.sh for the same idea in the other images). Started with
# --user, it just runs the control plane. Only busybox is on this image, so every tool is called through it.
set -eu
bb=/usr/local/bin/busybox
dir="${AGENTFIELD_HOME:-/data}"
if [ "$($bb id -u)" = "0" ]; then
  $bb mkdir -p "$dir"
  [ "$($bb stat -c %u "$dir")" = "65532" ] || $bb chown -R 65532:65532 "$dir"
  echo "agentfield: $dir belongs to 65532; running the control plane as uid 65532" >&2
  exec $bb setuidgid 65532:65532 /usr/local/bin/agentfield-server "$@"
fi
exec /usr/local/bin/agentfield-server "$@"

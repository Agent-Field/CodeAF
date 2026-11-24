#!/bin/sh
# Sourced by an entrypoint (never run alone), BEFORE it does anything else:
#
#     ORG_UID=10001 ORG_HOME=/home/codeaf ORG_DIRS="/data" . /opt/org/drop-root.sh
#
# Why: a platform volume (Railway's, a bind mount) arrives owned by root, and a process that
# starts as an unprivileged user cannot write to it. So the container starts as root, makes the
# volume folders belong to the service's user ONCE (a recursive chown only when the folder is
# not theirs yet, so a restart over a big workspace costs nothing), and then replaces itself
# with the same entrypoint running as that user. Nothing long-lived runs as root.
# A container started with --user already skips all of this.
if [ "$(id -u)" = "0" ]; then
  for dir in $ORG_DIRS; do
    mkdir -p "$dir"
    [ "$(stat -c %u "$dir")" = "$ORG_UID" ] || chown -R "$ORG_UID:$ORG_UID" "$dir" 2>/dev/null || true
  done
  echo "drop-root: ${ORG_DIRS:-no folders} belong to uid $ORG_UID; running $(basename "$0") as uid $ORG_UID" >&2
  exec setpriv --reuid="$ORG_UID" --regid="$ORG_UID" --clear-groups env HOME="$ORG_HOME" "$0" "$@"
fi

#!/bin/sh
# The hosted-chats machine's entrypoint (role `sessionhost`). It runs as root on purpose and holds only
# the five capabilities compose.yaml grants it; `codeaf sessionhost serve` narrows itself further at
# start and refuses to start chats unless it can keep them apart (it says so in its first log line).
# Nothing secret is baked into the image: the shared secret arrives as CODEAF_SESSIONHOST_SECRET.
set -eu
data="${CODEAF_SESSIONHOST_DATA:-/chats}"
mkdir -p "$data"
chmod 0755 "$data"
echo "sessionhost: serving hosted chats from $data as uid $(id -u)" >&2
exec codeaf sessionhost serve "$@"

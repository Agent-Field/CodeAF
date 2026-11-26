#!/bin/sh
# The codeaf image's entrypoint. The first argument picks the role:
#   server   the organization server, `codeaf factory serve`, in team mode
#   runner   the factory runner that connects the organization's work to AgentField
#   sessionhost  the machine hosted chats run on (stays root: see below)
#   <other>  run that command as given (a shell, `codeaf --help`)
set -eu
# CODEAF_ROLE picks the role where a platform cannot pass arguments (Railway sets it as a variable).
role="${CODEAF_ROLE:-${1:-server}}"
# THE SESSIONHOST IS THE ONE ROLE THAT DOES NOT DROP ROOT: it exists to switch to a user of its own
# for every hosted chat, which only root can do. It is chosen BEFORE the hand-off below; it holds
# five capabilities and no organization secret (compose.yaml says which and why), and it narrows
# itself again at start (`codeaf sessionhost serve`).
if [ "$role" = "sessionhost" ]; then
  [ "$#" -gt 0 ] && shift
  exec /opt/org/sessionhost.sh "$@"
fi
ORG_UID=10001 ORG_HOME=/home/codeaf ORG_DIRS="/data" . /opt/org/drop-root.sh
[ "$#" -gt 0 ] && shift
case "$role" in
  server) exec /opt/org/serve.sh "$@" ;;
  runner) exec python3 /opt/org/runner.py "$@" ;;
  *) exec "$role" "$@" ;;
esac

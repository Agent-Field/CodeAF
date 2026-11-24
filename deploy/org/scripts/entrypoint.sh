#!/bin/sh
# The codeaf image's entrypoint. The first argument picks the role:
#   server   the organization server, `codeaf factory serve`, in team mode
#   runner   the factory runner that connects the organization's work to AgentField
#   <other>  run that command as given (a shell, `codeaf --help`)
set -eu
ORG_UID=10001 ORG_HOME=/home/codeaf ORG_DIRS="/data" . /opt/org/drop-root.sh
# CODEAF_ROLE picks the role where a platform cannot pass arguments (Railway sets it as a variable).
role="${CODEAF_ROLE:-${1:-server}}"
[ "$#" -gt 0 ] && shift
case "$role" in
  server) exec /opt/org/serve.sh "$@" ;;
  runner) exec python3 /opt/org/runner.py "$@" ;;
  *) exec "$role" "$@" ;;
esac

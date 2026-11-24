#!/bin/sh
# Creates .env from .env.example with fresh random secrets. Safe to re-run: it never
# overwrites an existing .env, and never prints a secret.
set -eu
cd "$(dirname "$0")"
if [ -f .env ]; then
  echo ".env already exists; leaving it as it is."
  exit 0
fi
random() { od -An -N32 -tx1 /dev/urandom | tr -d ' \n'; }
umask 077
sed -e "s/^AGENTFIELD_API_KEY=.*/AGENTFIELD_API_KEY=$(random)/" \
    -e "s/^CODEAF_FACTORY_TOKEN=.*/CODEAF_FACTORY_TOKEN=$(random)/" .env.example > .env
echo "Wrote .env with a new AgentField key and organization token."
echo "Add OPENROUTER_API_KEY (and GH_TOKEN) to run real work, or use the keyless smoke test (see README.md)."

#!/bin/sh
# Creates .env from .env.example with fresh random secrets. Safe to re-run: it never
# overwrites an existing .env, and never prints a secret.
set -eu
cd "$(dirname "$0")"
random() { od -An -N32 -tx1 /dev/urandom | tr -d ' \n'; }
umask 077
if [ -f .env ]; then
  # An .env from before hosted chats has no secret for them. It gets one, once, appended; nothing
  # else in the file is touched, and a secret that is already there is never replaced.
  if ! grep -q '^CODEAF_HOSTED_SECRET=.' .env; then
    grep -v '^CODEAF_HOSTED_SECRET=$' .env > .env.new || true
    printf 'CODEAF_HOSTED_SECRET=%s\n' "$(random)" >> .env.new
    mv .env.new .env
    echo ".env already exists; added a secret for hosted chats and left the rest as it is."
    exit 0
  fi
  echo ".env already exists; leaving it as it is."
  exit 0
fi
sed -e "s/^AGENTFIELD_API_KEY=.*/AGENTFIELD_API_KEY=$(random)/" \
    -e "s/^CODEAF_FACTORY_TOKEN=.*/CODEAF_FACTORY_TOKEN=$(random)/" \
    -e "s/^CODEAF_HOSTED_SECRET=.*/CODEAF_HOSTED_SECRET=$(random)/" .env.example > .env
echo "Wrote .env with a new AgentField key, organization token and hosted-chats secret."
echo "Add OPENROUTER_API_KEY (and GH_TOKEN) to run real work, or use the keyless smoke test (see README.md)."

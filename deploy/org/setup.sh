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
  added=
  if ! grep -q '^CODEAF_HOSTED_SECRET=.' .env; then
    grep -v '^CODEAF_HOSTED_SECRET=$' .env > .env.new || true
    printf 'CODEAF_HOSTED_SECRET=%s\n' "$(random)" >> .env.new
    mv .env.new .env
    added="a secret for hosted chats"
  fi
  # The server key seals people's own keys on disk. It is made once and never replaced: a new one would make
  # everything stored under the old one unreadable.
  if ! grep -q '^CODEAF_SERVER_KEY=.' .env; then
    grep -v '^CODEAF_SERVER_KEY=$' .env > .env.new || true
    printf 'CODEAF_SERVER_KEY=%s\n' "$(random)" >> .env.new
    mv .env.new .env
    added="${added:+$added and }a server key for people's own keys"
  fi
  if [ -n "$added" ]; then
    echo ".env already exists; added $added and left the rest as it is."
  else
    echo ".env already exists; leaving it as it is."
  fi
  exit 0
fi
sed -e "s/^AGENTFIELD_API_KEY=.*/AGENTFIELD_API_KEY=$(random)/" \
    -e "s/^CODEAF_FACTORY_TOKEN=.*/CODEAF_FACTORY_TOKEN=$(random)/" \
    -e "s/^CODEAF_HOSTED_SECRET=.*/CODEAF_HOSTED_SECRET=$(random)/" \
    -e "s/^CODEAF_SERVER_KEY=.*/CODEAF_SERVER_KEY=$(random)/" .env.example > .env
echo "Wrote .env with a new AgentField key, organization token, hosted-chats secret and server key."
echo "Keep a copy of CODEAF_SERVER_KEY somewhere safe: the keys people store are sealed with it and cannot be opened without it."
echo "Add OPENROUTER_API_KEY (and GH_TOKEN) to run real work, or use the scripted model to try it first (README.md, \"Without a model key\")."

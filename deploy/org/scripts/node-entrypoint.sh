#!/bin/sh
# Entrypoint of the reasoner images (swe-af, pr-af). Two jobs, then the node:
#   1. a node that is sent a key as a mounted file or a platform variable never
#      has it baked into the image; a *_FILE variable is read into the plain one;
#   2. git is told who it is and to sign in with the GitHub token when there is one.
set -eu
ORG_UID="${ORG_UID:-10002}" ORG_HOME="${ORG_HOME:-/home/sweaf}" ORG_DIRS="${ORG_DIRS:-/workspaces}" . /opt/org/drop-root.sh

for name in AGENTFIELD_API_KEY OPENROUTER_API_KEY GH_TOKEN; do
  eval "file=\${${name}_FILE:-}"
  if [ -n "$file" ] && [ -r "$file" ]; then
    eval "export ${name}=\$(cat \"\$file\")"
  fi
done

git config --global user.name "${GIT_AUTHOR_NAME:-codeaf org}"
git config --global user.email "${GIT_AUTHOR_EMAIL:-codeaf-org@users.noreply.github.com}"
if [ -n "${GH_TOKEN:-}" ] && command -v gh >/dev/null 2>&1; then
  gh auth setup-git --hostname github.com --force >/dev/null 2>&1 || true
fi

# The keyless smoke test needs a repository for SWE-AF to plan against, on THIS container's disk
# (the runner that creates the job is another container). Only when asked to, and only once.
if [ -n "${SMOKE_FIXTURE_REPO:-}" ] && [ ! -d "$SMOKE_FIXTURE_REPO/.git" ]; then
  mkdir -p "$SMOKE_FIXTURE_REPO" \
    && printf '# smoke fixture\n' > "$SMOKE_FIXTURE_REPO/README.md" \
    && git -C "$SMOKE_FIXTURE_REPO" init -q -b main \
    && git -C "$SMOKE_FIXTURE_REPO" add README.md \
    && git -C "$SMOKE_FIXTURE_REPO" commit -q -m "fixture"
fi

exec "$@"

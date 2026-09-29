#!/usr/bin/env bash
# clang-format clean, judged the way the project itself judges it: the format
# tool the project pins, the project's own style file, a dry run that fails on
# the first non-compliant file. Runs BEFORE the build, because the project's
# own compile target reformats sources in place — a check after it could never
# fail, and a criterion that cannot fail grades nothing.
#
# Runs inside the verifier container, in the task tree. The graded patch is
# already applied; HEAD still names the base commit, so the changed files are
# the worktree's diff plus its untracked additions.
set -uo pipefail
cd "${REPO_DIR:-/root/repos/jsonschema}"
STYLE="vendor/core/cmake/common/targets/clang-format.json"

files="$(git diff --name-only HEAD -- 'src/*.cc' 'src/*.h' 2>/dev/null || true)"
files="$files $(git ls-files --others --exclude-standard -- 'src/*.cc' 'src/*.h' 2>/dev/null || true)"

changed=0
for f in $files; do
  [ -f "$f" ] || continue
  changed=$((changed + 1))
  if ! clang-format --style="file:$STYLE" --dry-run -Werror "$f"; then
    echo "clang-format: $f is not clean"
    exit 1
  fi
done

if [ "$changed" = 0 ]; then
  echo "clang-format: no source files changed — nothing to check"
fi
exit 0
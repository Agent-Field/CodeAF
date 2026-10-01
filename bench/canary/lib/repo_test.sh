#!/usr/bin/env bash
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=repo.sh
source "$HERE/repo.sh"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
SOURCE="$TMP/source"
REMOTE="$TMP/remote.git"
WORK="$TMP/work"
WORK_HEAD="$TMP/work-head"

# Native Git on MSYS needs drive-letter paths; Unix Git uses paths unchanged.
git_path() {
  if command -v cygpath >/dev/null 2>&1; then
    cygpath -m "$1"
  else
    printf '%s\n' "$1"
  fi
}
SOURCE_GIT="$(git_path "$SOURCE")"
REMOTE_GIT="$(git_path "$REMOTE")"
WORK_GIT="$(git_path "$WORK")"
WORK_HEAD_GIT="$(git_path "$WORK_HEAD")"

git init -q -b main "$SOURCE_GIT"
git -C "$SOURCE_GIT" config user.name fixture
git -C "$SOURCE_GIT" config user.email fixture@example.invalid
printf 'base\n' >"$SOURCE/file.txt"
git -C "$SOURCE_GIT" add file.txt
git -C "$SOURCE_GIT" commit -q -m base
BASE="$(git -C "$SOURCE_GIT" rev-parse HEAD)"

git -C "$SOURCE_GIT" switch -q -c feat/gold-fix
printf 'gold fix\n' >>"$SOURCE/file.txt"
git -C "$SOURCE_GIT" commit -q -am 'gold fix'
GOLD="$(git -C "$SOURCE_GIT" rev-parse HEAD)"

git -C "$SOURCE_GIT" switch -q main
printf 'newer main\n' >>"$SOURCE/file.txt"
git -C "$SOURCE_GIT" commit -q -am 'newer main'

git clone -q --bare "$SOURCE_GIT" "$REMOTE_GIT"
fetch_only_tree "$WORK_GIT" "$REMOTE_GIT" "$BASE"

[ "$(git -C "$WORK_GIT" rev-parse HEAD)" = "$BASE" ] || {
  echo "working tree did not land on the requested base" >&2
  exit 1
}
[ -z "$(git -C "$WORK_GIT" for-each-ref --format='%(refname)' refs/remotes)" ] || {
  echo "fetch-only tree exposed remote branches" >&2
  git -C "$WORK_GIT" branch -r >&2
  exit 1
}
[ "$(git -C "$WORK_GIT" rev-list --all)" = "$BASE" ] || {
  echo "git log --all reached commits outside the requested base" >&2
  git -C "$WORK_GIT" log --all --oneline >&2
  exit 1
}
if git -C "$WORK_GIT" cat-file -e "$GOLD^{commit}" 2>/dev/null; then
  echo "fetch-only tree can read the upstream gold-fix commit" >&2
  exit 1
fi

fetch_only_tree "$WORK_HEAD_GIT" "$REMOTE_GIT" HEAD
REMOTE_HEAD="$(git --git-dir="$REMOTE_GIT" rev-parse HEAD)"
[ "$(git -C "$WORK_HEAD_GIT" rev-parse HEAD)" = "$REMOTE_HEAD" ] || {
  echo "HEAD fetch did not land on the remote default" >&2
  exit 1
}
[ "$(git -C "$WORK_HEAD_GIT" rev-list --all)" = "$REMOTE_HEAD" ] || {
  echo "HEAD fetch can reach commits other than the requested HEAD" >&2
  exit 1
}
[ -z "$(git -C "$WORK_HEAD_GIT" for-each-ref --format='%(refname)' refs/remotes)" ] || {
  echo "HEAD fetch exposed remote branches" >&2
  exit 1
}
if git -C "$WORK_HEAD_GIT" cat-file -e "$GOLD^{commit}" 2>/dev/null; then
  echo "HEAD fetch can read the side-branch gold fix" >&2
  exit 1
fi

RUN="$HERE/../../run.sh"
CELL="$HERE/cell.sh"
ONEROAD="$HERE/../../oneroad/lib/corpus.sh"
grep -q 'source "$BENCH_ROOT/canary/lib/repo.sh"' "$RUN" || {
  echo "bench/run.sh does not source the shared fetch-only helper" >&2
  exit 1
}
grep -q 'fetch_only_tree "$dir" "$REPO"' "$RUN" || {
  echo "bench/run.sh does not use the shared fetch-only helper" >&2
  exit 1
}
if grep -q 'git clone' "$RUN"; then
  echo "bench/run.sh grew a second full-clone path" >&2
  exit 1
fi
grep -q 'source "$HERE/repo.sh"' "$CELL" && grep -q 'fetch_only_tree "$WORK"' "$CELL" || {
  echo "canary cell does not use the shared fetch-only helper" >&2
  exit 1
}
grep -q 'source "$CORPUS_HERE/../../canary/lib/repo.sh"' "$ONEROAD" \
  && grep -q 'fetch_only_tree "$dir" "$REPO"' "$ONEROAD" || {
  echo "oneroad corpus does not use the shared fetch-only helper" >&2
  exit 1
}

echo "fetch-only clone regression: ok"

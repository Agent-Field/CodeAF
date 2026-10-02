#!/usr/bin/env bash
# Builds a fresh, deterministic workspace folder full of awkward things (git state, odd names,
# links, modes, big files) so that a move between two machines can be checked byte for byte.
# Usage: fidelity-fixture.sh <dir>
# Works with bash 3.2 (macOS) and needs only git and python3.
set -eu

SCRATCH=/tmp/claude-1001/-home-santosh/aea61d84-87f0-47fc-afde-ed7141b18d38/scratchpad

# Why: the script runs rm -rf on its argument, so it must prove the target is a throwaway place
# before it deletes anything; /private/tmp is what /tmp resolves to on macOS.
is_allowed_root() {
  case "$1" in
    /tmp/* | /private/tmp/* | /home/santosh/caf-vcont-rig* | /home/santosh/caf-vfid-rig* | "$SCRATCH"/*) return 0 ;;
  esac
  return 1
}

resolve() {
  python3 -c 'import os,sys;print(os.path.realpath(sys.argv[1]))' "$1"
}

# Why: read-only and 0000-mode entries from an earlier run would make rm -rf fail half way.
wipe() {
  if [ -e "$1" ] || [ -L "$1" ]; then
    chmod -R u+rwx "$1" 2>/dev/null || true
    rm -rf "$1"
  fi
}

# Why: pinned identity and environment make every run produce the same commit ids.
git_env() {
  export GIT_CONFIG_NOSYSTEM=1
  export GIT_AUTHOR_NAME=Fixture GIT_AUTHOR_EMAIL=fixture@example.invalid
  export GIT_COMMITTER_NAME=Fixture GIT_COMMITTER_EMAIL=fixture@example.invalid
}

# Why: a fixed commit date per step keeps commit ids identical between runs.
at_day() {
  export GIT_AUTHOR_DATE="2026-01-0$1T12:00:00+0000"
  export GIT_COMMITTER_DATE="2026-01-0$1T12:00:00+0000"
}

# Why: file:// submodules are blocked by default, and signing would need a key.
g() {
  git -c commit.gpgsign=false -c protocol.file.allow=always "$@"
}

commit_day() {
  at_day "$1"
  g commit -q -m "$2"
}

init_repo() {
  git init -q --template= "$1"
  git -C "$1" symbolic-ref HEAD refs/heads/main
}

build_history() {
  mkdir -p src docs svc/api
  printf 'alpha\n' > src/a.txt
  printf '# docs\n' > docs/b.md
  printf 'c\n' > docs/c.md
  printf 'dist/\nnode_modules/\n.env\n.env.local\n' > .gitignore
  g add .gitignore src docs
  commit_day 1 "first commit"
  printf 'beta\n' >> src/a.txt
  g add src/a.txt
  commit_day 2 "second commit"
  g checkout -q -b feature
  printf 'feature work\n' > docs/feature.md
  g add docs/feature.md
  commit_day 3 "feature commit"
  g checkout -q main
  printf 'gamma\n' >> src/a.txt
  g add src/a.txt
  commit_day 4 "third commit"
}

# Why: a submodule is the git feature most likely to break when only files are moved, but the
# add needs a second repo; if git refuses offline we say so and carry on.
add_submodule() {
  local sub="$1.subsrc"
  wipe "$sub"
  init_repo "$sub"
  printf 'sub content\n' > "$sub/sub.txt"
  git -C "$sub" add sub.txt
  (cd "$sub" && at_day 1 && g commit -q -m "sub first")
  if g submodule add -q "$sub" vendor/sub > /dev/null 2>&1; then
    commit_day 5 "add submodule"
  else
    echo "SKIP submodule"
    g reset -q
    rm -rf vendor .gitmodules .git/modules
  fi
}

# Why: the stash, the staged change, the unstaged change and the untracked file are the
# uncommitted states a user would be upset to lose in a move.
build_uncommitted_state() {
  at_day 6
  g tag -a v1 -m "release one"
  printf 'wip\n' >> src/a.txt
  g stash push -q -m fixture-stash
  printf 'staged\n' >> docs/b.md
  g add docs/b.md
  printf 'unstaged\n' >> docs/c.md
  printf 'untracked\n' > untracked.txt
}

build_names() {
  mkdir -p names
  printf 'nfc\n' > "names/$(printf 'caf\303\251')-nfc.txt"
  printf 'nfd\n' > "names/$(printf 'cafe\314\201')-nfd.txt"
  # Why: the same visible word in two byte spellings is the pair a normalising filesystem merges into one entry.
  # Why: a machine that folds the two spellings cannot hold both (see BENCH-MOVE), so the pair joins the fixture only when asked for.
  if [ -n "${FIXTURE_NAME_COLLISIONS:-}" ]; then
    printf 'same-nfc\n' > "names/same-$(printf 'caf\303\251').txt"
    printf 'same-nfd\n' > "names/same-$(printf 'cafe\314\201').txt"
  fi
  if [ "$(ls names | grep -c '^same-')" -lt 2 ]; then
    echo "SKIP same-word NFC/NFD pair (this filesystem folds the two spellings)"
  fi
  printf 'emoji\n' > "names/rocket-$(printf '\360\237\232\200').txt"
  printf 'spaces\n' > "names/name with  spaces.txt"
  printf 'dash\n' > "./-leading dash.txt"
  printf 'lower\n' > Readme.md
  if [ -n "${FIXTURE_NAME_COLLISIONS:-}" ]; then printf 'UPPER\n' > README.md; fi
  if [ "$(ls | grep -ci '^readme\.md$')" -lt 2 ]; then
    echo "SKIP case-collision (this filesystem is case-insensitive)"
  fi
}

# Why: a 3-deep empty chain, and a path past 200 characters, are the shapes that sync tools
# most often drop or truncate.
build_shapes() {
  mkdir -p empty-chain/lvl1/lvl2
  local deep="deep" i
  for i in 1 2 3 4 5 6 7 8 9 10 11; do
    deep="$deep/$(printf 'd%02d_abcdefghijklmnop' "$i")"
  done
  mkdir -p "$deep"
  printf 'deep leaf\n' > "$deep/leaf.txt"
}

# Why: a seeded generator makes the big files reproducible; 300 MiB is bigger than one sync frame.
make_big() {
  python3 - "$1" "$2" "$3" << 'PY'
import random, sys
path, mib, seed = sys.argv[1], int(sys.argv[2]), int(sys.argv[3])
block = bytearray(random.Random(seed).getrandbits(8 << 20).to_bytes(1 << 20, "little"))
with open(path, "wb") as out:
    for n in range(mib):
        block[:8] = n.to_bytes(8, "little")
        out.write(block)
PY
}

build_binaries() {
  mkdir -p big
  make_big big/50mb.bin 50 50
  make_big big/300mb.bin 300 300
}

build_links() {
  ln -s src/a.txt link-rel
  ln -s "$1/src/a.txt" link-abs
  ln -s no-such-target link-dangling
  ln -s /etc/hostname link-outside
  ln -s src link-dir
  printf 'hardlinked\n' > hl-a
  ln hl-a hl-b
}

build_ignored_and_secrets() {
  mkdir -p dist node_modules/pkg-a node_modules/pkg-b node_modules/pkg-c/lib
  printf 'console.log(1)\n' > dist/out.js
  printf 'LOCAL=1\n' > .env.local
  printf 'SECRET=1\n' > .env
  printf 'API_SECRET=1\n' > svc/api/.env
  local d i
  for d in pkg-a pkg-b pkg-c pkg-c/lib; do
    for i in 1 2 3 4 5 6 7 8 9 10; do
      printf 'module %s %s\n' "$d" "$i" > "node_modules/$d/file$i.js"
    done
  done
}

# Why: modes go last so that building the fixture never trips over its own restrictions.
build_modes() {
  mkdir -p bin perm ro-dir
  printf '#!/bin/sh\necho hi\n' > bin/run.sh
  printf 'zero\n' > perm/zero-mode.txt
  printf 'locked\n' > ro-dir/inside.txt
  chmod 0755 bin/run.sh
  # Why: one unreadable file makes every seal of the tree fail (see BENCH-MOVE), so it joins the fixture only when asked for.
  if [ -n "${FIXTURE_ZERO_MODE:-}" ]; then chmod 0000 perm/zero-mode.txt; else chmod 0400 perm/zero-mode.txt; fi
  chmod 0600 .env
  chmod 0640 svc/api/.env
  # Why: a read-only directory that holds files stops the receiving machine from rebuilding the tree (see BENCH-MOVE), so it joins the fixture only when asked for.
  if [ -n "${FIXTURE_RO_DIR:-}" ]; then chmod 0555 ro-dir; fi
}

main() {
  local dir
  dir="$(resolve "${1:?usage: fidelity-fixture.sh <dir>}")"
  is_allowed_root "$dir" || { echo "refusing: $dir is outside the allowed scratch roots" >&2; exit 2; }
  wipe "$dir"
  wipe "$dir.subsrc"
  git_env
  mkdir -p "$dir"
  init_repo "$dir"
  cd "$dir"
  build_history
  add_submodule "$dir"
  build_uncommitted_state
  build_names
  build_shapes
  build_binaries
  build_ignored_and_secrets
  build_links "$dir"
  build_modes
  echo "fixture ready $dir"
}

main "$@"

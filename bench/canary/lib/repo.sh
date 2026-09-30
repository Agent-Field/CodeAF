#!/usr/bin/env bash

# Build a working tree that can see only one fetched commit. The remote is used
# as a one-shot object source rather than configured as `origin`, so upstream
# branches and later fixes never become refs the benchmark door can inspect.
fetch_only_tree() {
  local dir="$1" remote="$2" base="$3"
  rm -rf "$dir"
  git init -q -b main "$dir" || return 1
  git -C "$dir" fetch -q --depth 1 "$remote" "$base" || return 1
  git -C "$dir" reset -q --hard FETCH_HEAD || return 1
}

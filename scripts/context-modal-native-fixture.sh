#!/usr/bin/env bash
# Build a deterministic context-browser tree and emit genuine terminal mouse reports.
# This script never launches aforge, changes an existing profile, or contacts an engine.
set -euo pipefail

usage() {
  echo "usage: $0 setup ROOT | motion COL ROW | click COL ROW | wheel COL ROW up|down" >&2
  exit 2
}

command=${1:-}
case "$command" in
setup)
  root=${2:-}
  test -n "$root" || usage
  mkdir -p "$root/alpha sibling" \
    "$root/modal project/child folder/nested" \
    "$root/modal project/source" \
    "$root/modal project/long" \
    "$root/omega sibling"
  for n in $(seq -w 1 28); do
    printf 'alpha row %s\n' "$n" > "$root/alpha sibling/a${n}.txt"
  done
  for n in $(seq -w 1 40); do
    printf 'long row %s\n' "$n" > "$root/modal project/long/list-${n}.txt"
  done
  printf 'package nested\n\nfunc Deep() string { return "raw path identity" }\n' \
    > "$root/modal project/child folder/nested/deep.go"
  printf 'package main\n\nimport "fmt"\n\nfunc main() {\n\tfmt.Println("modal preview")\n}\n' \
    > "$root/modal project/source/main.go"
  printf 'first line\nsecond line\nthird line\n' > "$root/modal project/notes.txt"
  # A 1x1 opaque PNG keeps the fixture portable; native review may also copy a photo here.
  printf '%s' 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=' \
    | base64 --decode > "$root/modal project/pixel.png" 2>/dev/null || \
    printf '%s' 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=' \
      | base64 -D > "$root/modal project/pixel.png"
  printf '%s\n' "$root/modal project"
  ;;
motion)
  test "$#" -eq 3 || usage
  printf '\033[<35;%s;%sM' "$2" "$3"
  ;;
click)
  test "$#" -eq 3 || usage
  printf '\033[<0;%s;%sM\033[<0;%s;%sm' "$2" "$3" "$2" "$3"
  ;;
wheel)
  test "$#" -eq 4 || usage
  case "$4" in
  up) button=64 ;;
  down) button=65 ;;
  *) usage ;;
  esac
  printf '\033[<%s;%s;%sM' "$button" "$2" "$3"
  ;;
*) usage ;;
esac

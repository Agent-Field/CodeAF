#!/usr/bin/env bash
# Proves that no secret can enter the codeaf image's build context: it plants fake .env files, key files, a profile folder
# and a notes file all over a copy of the repository's files, builds codeaf.Dockerfile's `context-probe` target (which
# copies the whole context into an otherwise empty image) and checks that none of the planted files is in it while the
# files the source build needs are. The planted files exist only in a temporary tar; nothing is written into the tree.
#
#     DOCKER=docker ./context-check.sh [REPO_ROOT]        # DOCKER is optional (the wrapper that reaches a docker engine)
set -eu
root=${1:-$(git rev-parse --show-toplevel)}
DOCKER=${DOCKER:-docker}
read -r -a D <<<"$DOCKER"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

planted=(
  .env
  .env.local
  deploy/org/.env
  deploy/org/scripts/.env
  internal/.env.production
  cmd/codeaf/prod.env
  .codeaf/config.json
  .claude/settings.json
  .aws/credentials
  .ssh/id_ed25519
  home/someone/.codeaf/profile.json
  cmd/codeaf/id_rsa
  internal/probe.pem
  internal/token.key
  docs/private-notes.md
  notes.txt
)
required=(
  go.mod
  go.sum
  Makefile
  cmd/codeaf/main.go
  internal/manual/manual.go
  third_party/raft/go.mod
  deploy/org/scripts/entrypoint.sh
  deploy/org/scripts/sessionhost.sh
)

mkdir -p "$work/plant"
for path in "${planted[@]}"; do
  mkdir -p "$work/plant/$(dirname "$path")"
  echo "planted-fake-$RANDOM" >"$work/plant/$path"
done

# The context is every file git knows (tracked, or new and not ignored) plus the planted ones: what a build on a
# checkout, a laptop or a CI runner could be handed.
(cd "$root" && git ls-files -z --cached --others --exclude-standard | tar --null -T - -cf -) >"$work/context.tar"
tar -C "$work/plant" -rf "$work/context.tar" .

"${D[@]}" build -f deploy/org/codeaf.Dockerfile --target context-probe -o type=tar,dest=- - <"$work/context.tar" >"$work/probe.tar" 2>"$work/build.log" || {
  echo "context check: the probe image did not build:" >&2
  tail -n 15 "$work/build.log" >&2
  exit 2
}
tar -tf "$work/probe.tar" | sed 's|^\./||' >"$work/listing"

bad=0
for path in "${planted[@]}"; do
  if grep -qx "ctx/$path" "$work/listing"; then
    echo "context check: FAIL: the planted file $path entered the build context" >&2
    bad=1
  fi
done
for path in "${required[@]}"; do
  if ! grep -qx "ctx/$path" "$work/listing"; then
    echo "context check: FAIL: $path, which the build needs, is not in the build context" >&2
    bad=1
  fi
done
echo "context check: $(grep -c '[^/]$' "$work/listing") files in the build context, ${#planted[@]} planted files kept out, ${#required[@]} required files present"
[ "$bad" = 0 ] && echo "context check: PASS"
exit "$bad"

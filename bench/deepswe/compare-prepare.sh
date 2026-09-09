#!/usr/bin/env bash
# Install a pinned comparator once, then freeze the resulting container image.
set -euo pipefail
out="$HOME/bench-artifacts/af653-complex-20260905"
mkdir -p "$out"
[[ ! -f "$out/runtime-image.txt" ]] || exit 0
name=af653-complex-runtime-20260905
base=sha256:886e64cad852b7f75bee491f4550fff253e1c9414e678bda0d233764454af51f
trap 'docker rm -f "$name" >/dev/null 2>&1 || true' EXIT
docker run -d --name "$name" --platform linux/amd64 --cpus 2 --memory 8192m \
  --entrypoint /bin/bash "$base" -c 'sleep 1800' > "$out/runtime-container.txt"
timeout 900 docker exec "$name" npm install -g @earendil-works/pi-coding-agent@0.84.2 \
  > "$out/runtime-install.log" 2>&1
docker exec "$name" bash -c 'node --version; npm --version; pi --version; npm ls -g --all --json' \
  > "$out/runtime-toolchain.txt" 2>&1
docker commit "$name" > "$out/runtime-image.txt"
sha256sum bin/aforge > "$out/aforge-binary.sha256"
cp bench/deepswe/compare-cell.sh "$out/compare-cell.snapshot.sh"
echo 'Container runtime ready.'

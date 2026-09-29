#!/usr/bin/env bash
# Stage one campaign host from the committed rig, credential-free.
#
#   bench/frontiercode/gcp-stage.sh --plan                       print what would happen, touch nothing
#   bench/frontiercode/gcp-stage.sh --check  <instance> <shard>  run every gate, transfer nothing
#   bench/frontiercode/gcp-stage.sh --stage  <instance> <shard>  transfer the rig and the pinned binary, warm images
#
# What --stage sends: a git bundle of the committed rig at the pinned rig
# commit (the bundle carries a named ref, never a bare revision), the pinned
# bin/codeaf for linux/amd64, and this script. It sends no credential and
# prints none. On the host, in order and each on its own line so a failure
# cannot hide inside an && chain: install Go if absent; check the rig out at
# its pinned commit; install and re-hash the binary; pin the corpus and the
# shard to their manifest hashes; build the egress proxy, bind it on the docker
# bridge where the campaign's containers can reach it, and verify the listening
# address rather than a localhost health check; then start one background image
# warm loop guarded by a PID file the child writes itself.
#
# <shard> is a shard number from the manifest, or a path to a task list.
set -euo pipefail
FC_SCRIPT=gcp-stage.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/gcp-lib.sh"

MODE="${1:-}"
case "$MODE" in --plan|--check|--stage) ;; *) sed -n '2,16p' "$0" >&2; exit 2 ;; esac
INSTANCE="${2:-}"; SHARD="${3:-}"
if [ "$MODE" != --plan ]; then
  [ -n "$INSTANCE" ] || fc_die "instance is required"
  [ -n "$SHARD" ] || fc_die "shard is required"
fi

fc_load_manifest
fc_require_keys .gcp.instance_prefix .gcp.zone .codeaf_commit .codeaf_sha256 \
  .codeaf_local_binary .rig_commit .corpus .corpus_sha256 .host_shards

PROJECT="${GCP_PROJECT:-$(fc_get '.gcp.project')}"
ZONE="$(fc_get '.gcp.zone')"
REMOTE_ROOT="${FC_REMOTE_ROOT:-/home/$USER/codeaf-rig}"

# ── local gates ─────────────────────────────────────────────────────────────
# Refuse a dirty rig: any uncommitted tracked file, and any untracked file
# inside bench/frontiercode/ (a campaign file forgotten before commit). An
# untracked file elsewhere belongs to another lane sharing the checkout, cannot
# enter the bundle, and is reported rather than treated as blocking.
dirty="$(git -C "$FC_REPO_ROOT" status --porcelain --untracked-files=no)"
[ -z "$dirty" ] || { echo "refusing to stage an uncommitted rig:" >&2; echo "$dirty" >&2; exit 3; }
stray="$(git -C "$FC_REPO_ROOT" ls-files --others --exclude-standard -- bench/frontiercode)"
[ -z "$stray" ] || { echo "refusing to stage: uncommitted file(s) inside bench/frontiercode/:" >&2; echo "$stray" >&2; exit 3; }
foreign="$(git -C "$FC_REPO_ROOT" ls-files --others --exclude-standard | grep -v '^bench/frontiercode/' || true)"
[ -z "$foreign" ] || { echo "note: untracked file(s) outside bench/frontiercode/, not staged and not in the bundle:"; echo "$foreign" | sed 's/^/  /'; }

# The pinned rig commit travels as a ref inside the bundle. A bundle names
# refs, not bare revisions, so the commit must be the tip of a branch the
# bundle will carry — which is the committed HEAD of the checkout being staged.
rig_commit="$(fc_get '.rig_commit')"
if [ "$rig_commit" = "HEAD" ]; then rig_commit="$(git -C "$FC_REPO_ROOT" rev-parse HEAD)"; fi
git -C "$FC_REPO_ROOT" cat-file -e "$rig_commit^{commit}" 2>/dev/null || fc_die "rig commit $rig_commit is not in this checkout"
head_commit="$(git -C "$FC_REPO_ROOT" rev-parse HEAD)"
[ "$head_commit" = "$rig_commit" ] || fc_die "HEAD $head_commit is not the pinned rig commit $rig_commit — commit the rig, or update the manifest"
bundle_ref="$(git -C "$FC_REPO_ROOT" for-each-ref --format='%(refname)' --contains "$rig_commit" refs/heads | awk 'NR==1{print}')"
[ -n "$bundle_ref" ] || fc_die "no branch contains the pinned rig commit $rig_commit; a bundle must name a ref"

local_bin="$FC_REPO_ROOT/$(fc_get '.codeaf_local_binary')"
[ -f "$local_bin" ] || fc_die "pinned binary is missing: $(fc_get '.codeaf_local_binary') (build it with GOOS=linux GOARCH=amd64 go build)"
codeaf_sha="$(fc_get '.codeaf_sha256')"
actual_sha="$(fc_sha256 "$local_bin")"
[ "$actual_sha" = "$codeaf_sha" ] || fc_die "local binary sha256 $actual_sha != manifest $codeaf_sha"

# The shard: a manifest shard number, or a path to a task list (how a small
# preregistered arm stages the handful of tasks it needs without a second path).
case "$SHARD" in
  '') shard_rel="" ;;
  *[!0-9]*)
    [ -f "$SHARD" ] || fc_die "shard must be a number or a task-list path: $SHARD"
    shard_rel="$SHARD"
    ;;
  *)
    shard_rel="$(jq -r --arg s "$SHARD" '.shard_files[$s] // empty' "$FC_MANIFEST_PATH")"
    [ -n "$shard_rel" ] || fc_die "manifest names no shard file for shard $SHARD"
    ;;
esac

echo "instance=$INSTANCE shard=${SHARD:-<all>} project=$PROJECT zone=$ZONE"
echo "rig commit=$rig_commit ref=$bundle_ref"
echo "binary=$(fc_get '.codeaf_local_binary') sha256=${codeaf_sha:0:12}..."
echo "corpus=$(fc_get '.corpus') sha256=$(fc_get '.corpus_sha256')"

if [ -n "$shard_rel" ]; then
  shard_sha="$(fc_sha256 "$FC_REPO_ROOT/$shard_rel")"
  want_shard_sha="$(jq -r --arg s "$SHARD" '.shard_sha256[$s] // empty' "$FC_MANIFEST_PATH" 2>/dev/null || true)"
  if [ -n "$want_shard_sha" ] && [ "$shard_sha" != "$want_shard_sha" ]; then
    fc_die "shard $SHARD hash $shard_sha != manifest $want_shard_sha"
  fi
  echo "shard file=$shard_rel sha256=$shard_sha"
fi

if [ "$MODE" = --plan ]; then
  echo "PLAN ONLY: local gates passed; no instance was contacted and nothing transferred"
  exit 0
fi

gcloud compute instances describe "$INSTANCE" --project="$PROJECT" --zone="$ZONE" \
  --format='value(status)' 2>/dev/null | grep -qx RUNNING \
  || fc_die "instance is not RUNNING: $INSTANCE (in $ZONE)"
if [ "$MODE" = --check ]; then
  echo "CHECK ONLY: local gates passed and the instance is RUNNING; nothing transferred"
  exit 0
fi

# ── the committed rig as a bundle, keyed by a ref ───────────────────────────
bundle="$(mktemp "${TMPDIR:-/tmp}/fc-rig.XXXXXX")"
remote_cfg="$(mktemp "${TMPDIR:-/tmp}/fc-stage.XXXXXX")"
remote_script="$(mktemp "${TMPDIR:-/tmp}/fc-stage-remote.XXXXXX.sh")"
trap 'rm -f "$bundle" "$remote_cfg" "$remote_script"' EXIT
git -C "$FC_REPO_ROOT" bundle create "$bundle" HEAD "$bundle_ref" >/dev/null

{
  printf 'FC_RIG_COMMIT=%s\n' "$rig_commit"
  printf 'FC_BUNDLE_REF=%s\n' "$bundle_ref"
  printf 'FC_CODEAF_SHA=%s\n' "$codeaf_sha"
  printf 'FC_CORPUS_REL=%s\n' "$(fc_get '.corpus')"
  printf 'FC_CORPUS_SHA=%s\n' "$(fc_get '.corpus_sha256')"
  printf 'FC_SHARD_REL=%s\n' "$shard_rel"
} > "$remote_cfg"

cat > "$remote_script" <<'REMOTE'
#!/usr/bin/env bash
# The host half of gcp-stage.sh. Each step on its own line fails loudly; this
# file is generated locally, transferred, and run once. It receives no secret.
set -euo pipefail
REMOTE_ROOT="$1"; BUNDLE="$2"; BIN="$3"; CFG="$4"
# shellcheck disable=SC1090
. "$CFG"
RIG="$REMOTE_ROOT/bench/frontiercode"

[ "$(uname -m)" = x86_64 ] || { echo "host is not x86_64: $(uname -m)" >&2; exit 3; }

# Go is needed to build the egress proxy and the rig's own images do not ship
# it. Install it only when absent, and pin the version the rig was built with.
if ! command -v go >/dev/null 2>&1; then
  echo "installing Go"
  curl -fsSL -o /tmp/go.tgz https://go.dev/dl/go1.25.14.linux-amd64.tar.gz || { echo "go download failed" >&2; exit 3; }
  sudo tar -C /usr/local -xzf /tmp/go.tgz || { echo "go extract failed" >&2; exit 3; }
  rm -f /tmp/go.tgz
  sudo ln -sf /usr/local/go/bin/go /usr/local/bin/go
fi
go version

# Check the rig out at its pinned commit. A checkout that already exists is
# fetched from the bundle and moved; the checkout is evidence, not a workspace.
if [ ! -d "$REMOTE_ROOT/.git" ]; then
  git clone --quiet "$BUNDLE" "$REMOTE_ROOT" || { echo "bundle clone failed" >&2; exit 3; }
fi
git -C "$REMOTE_ROOT" fetch --quiet "$BUNDLE" || { echo "bundle fetch failed" >&2; exit 3; }
git -C "$REMOTE_ROOT" checkout --quiet --detach "$FC_RIG_COMMIT" || { echo "rig checkout failed" >&2; exit 3; }
[ "$(git -C "$REMOTE_ROOT" rev-parse HEAD)" = "$FC_RIG_COMMIT" ] || { echo "rig HEAD is not the pinned commit" >&2; exit 3; }
echo "rig at $(git -C "$REMOTE_ROOT" rev-parse --short HEAD)"

# Install the pinned binary and re-hash it on the host: a transfer that
# corrupted a byte must refuse rather than run a different harness.
install -D -m 755 "$BIN" "$RIG/bin/codeaf" || { echo "binary install failed" >&2; exit 3; }
[ "$(sha256sum "$RIG/bin/codeaf" | cut -d' ' -f1)" = "$FC_CODEAF_SHA" ] || { echo "binary hash mismatch after copy" >&2; exit 3; }
echo "binary installed and re-hashed"

# Pin the corpus to the commit's own bytes. The manifest hashes are the law.
cd "$REMOTE_ROOT"
corpus_sha="$(find "$FC_CORPUS_REL" -type f -not -path '*/solution/*' -not -name '.DS_Store' | LC_ALL=C sort | while IFS= read -r f; do printf '%s  %s\n' "$(sha256sum "$f" | cut -d' ' -f1)" "$f"; done | sha256sum | cut -d' ' -f1)"
[ "$corpus_sha" = "$FC_CORPUS_SHA" ] || { echo "corpus hash $corpus_sha != manifest $FC_CORPUS_SHA" >&2; exit 3; }
if [ -n "$FC_SHARD_REL" ]; then
  [ -f "$REMOTE_ROOT/$FC_SHARD_REL" ] || { echo "shard file missing after checkout: $FC_SHARD_REL" >&2; exit 3; }
fi
echo "corpus pinned at ${FC_CORPUS_SHA:0:12}"

# The egress proxy: built from the committed source, bound on the docker bridge
# so a container reaches it at an address that is not the loopback health
# check. run.sh still starts the per-run proxy that the agent's own network
# uses; this campaign-level instance is the harness's logged exit.
bridge="$(docker network inspect bridge --format '{{(index .IPAM.Config 0).Gateway}}' 2>/dev/null || true)"
[ -n "$bridge" ] || bridge="172.17.0.1"
campaign_dir="$HOME/.fc-campaign"
mkdir -p "$campaign_dir"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$RIG/bin/egress-proxy-amd64" "$RIG/proxy/egress-proxy.go" || { echo "egress proxy build failed" >&2; exit 3; }
pidf="$campaign_dir/egress.pid"
if [ -s "$pidf" ] && kill -0 "$(cat "$pidf")" 2>/dev/null; then
  echo "egress proxy already running (pid $(cat "$pidf"))"
else
  setsid nohup bash -c "echo \$\$ > '$pidf'; exec '$RIG/bin/egress-proxy-amd64' -addr '$bridge:3128' -resolver 1.1.1.1:53 -log '$campaign_dir/egress-proxy.log'" </dev/null >>"$campaign_dir/egress.log" 2>&1 &
fi
for _ in 1 2 3 4 5 6 7 8 9 10; do
  ss -lnt 2>/dev/null | grep -q "$bridge:3128" && break
  sleep 1
done
ss -lnt 2>/dev/null | grep -q "$bridge:3128" || { echo "egress proxy is not listening on $bridge:3128" >&2; exit 3; }
curl -sf -m 5 -x "http://$bridge:3128" -o /dev/null http://example.com || { echo "egress proxy is not reachable at $bridge:3128" >&2; exit 3; }
echo "egress proxy listening on $bridge:3128 (verified)"

# One background image warm loop, guarded by a PID file the child writes before
# it execs. The guard cannot be pgrep over a command line: this whole script
# arrives as one ssh command whose text contains the warm invocation, so
# 'pgrep -f ...' would match the shell evaluating it and report a running loop
# that was never started. The child writes its own PID, so liveness is a fact
# about the process rather than about text. The loop's own files live outside
# the rig checkout so a staged worktree stays clean.
cat > "$campaign_dir/warm-images.sh" <<'WARM'
#!/usr/bin/env bash
set -uo pipefail
RIG="$1"; SHARD="$2"
source "$RIG/lib.sh"
while IFS= read -r t; do
  case "$t" in ''|'#'*) continue ;; esac
  if load_task "$t"; then
    ensure_env_image || { echo "warm: environment image failed for $t"; continue; }
    ensure_verify_image || { echo "warm: verifier image failed for $t"; continue; }
    echo "warm: $t cached"
  else
    echo "warm: no such task $t"
  fi
done < "$SHARD"
WARM
chmod +x "$campaign_dir/warm-images.sh"
warm_pidf="$campaign_dir/pull.pid"
if [ -s "$warm_pidf" ] && kill -0 "$(cat "$warm_pidf")" 2>/dev/null; then
  echo "image warm loop already running (pid $(cat "$warm_pidf"))"
else
  shard_file="${FC_SHARD_REL:-}"
  [ -n "$shard_file" ] || shard_file="$(ls "$RIG"/shards/shard-*.txt 2>/dev/null | head -1)"
  setsid nohup bash -c "echo \$\$ > '$warm_pidf'; exec '$campaign_dir/warm-images.sh' '$RIG' '$REMOTE_ROOT/$shard_file'" </dev/null >>"$campaign_dir/warm.log" 2>&1 &
  echo "image warm loop started in the background"
fi

nproc | sed 's/^/host cpus: /'
awk '/MemTotal/ {print "host memory: " int($2/1024/1024) " GiB"}' /proc/meminfo
REMOTE

scp_out="$(gcloud compute scp "$bundle" "$local_bin" "$remote_cfg" "$remote_script" "$INSTANCE:/tmp/" \
  --project="$PROJECT" --zone="$ZONE" --quiet 2>&1)" || { printf '%s\n' "$scp_out" | grep -v '^Warning'; fc_die "transfer to $INSTANCE failed"; }
printf '%s\n' "$scp_out" | grep -v '^Warning' || true

remote_bundle="/tmp/$(basename "$bundle")"
remote_bin="/tmp/$(basename "$local_bin")"
remote_cfg_path="/tmp/$(basename "$remote_cfg")"
remote_script_path="/tmp/$(basename "$remote_script")"
ssh_out="$(gcloud compute ssh "$INSTANCE" --project="$PROJECT" --zone="$ZONE" --quiet \
  --command="bash '$remote_script_path' '$REMOTE_ROOT' '$remote_bundle' '$remote_bin' '$remote_cfg_path'" 2>&1)" \
  || { printf '%s\n' "$ssh_out" | grep -v '^Warning: Permanently added'; fc_die "staging on $INSTANCE failed"; }
printf '%s\n' "$ssh_out" | grep -v '^Warning: Permanently added' || true

echo "staged $INSTANCE for shard ${SHARD:-<all>}; no credential was transferred and no attempt was launched"

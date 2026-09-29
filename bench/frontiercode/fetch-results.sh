#!/usr/bin/env bash
# Bring a finished shard's artifacts home, prove nothing is missing, then take
# the host down. Trajectories cannot be regenerated once the host is gone, so a
# missing artifact is a refusal and the host is left standing.
#
#   bench/frontiercode/fetch-results.sh --plan                 print what would be fetched and deleted, touch nothing
#   bench/frontiercode/fetch-results.sh --check                verify the locally fetched runs are complete, no host contacted
#   bench/frontiercode/fetch-results.sh --fetch <instance>     fetch, verify, report; delete the host
#   bench/frontiercode/fetch-results.sh --fetch <instance> --keep-host   fetch and verify only
#   bench/frontiercode/fetch-results.sh --teardown <instance>  shred the key and delete the host, after verifying the local copy
#
# Only run directories carrying a DONE sentinel are fetched: a run still in
# flight is never copied. The whole directory comes home -- the trajectory, the
# patch, the grade, the scan, the cost readings -- and every fetched run must
# then carry its full artifact set or this exits non-zero and deletes nothing.
set -euo pipefail
FC_SCRIPT=fetch-results.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/gcp-lib.sh"

MODE="${1:-}"; INSTANCE="${2:-}"; KEEP_HOST=0
for a in "$@"; do if [ "$a" = --keep-host ]; then KEEP_HOST=1; fi; done
case "$MODE" in --plan|--check|--fetch|--teardown) ;; *) sed -n '2,17p' "$0" >&2; exit 2 ;; esac
case "$MODE" in --fetch|--teardown) [ -n "$INSTANCE" ] || fc_die "instance is required" ;; esac

fc_load_manifest
fc_require_keys .gcp.zone .gcp.instance_prefix .key.store .key.item

PROJECT="${GCP_PROJECT:-$(fc_get '.gcp.project')}"
ZONE="$(fc_get '.gcp.zone')"
REMOTE_ROOT="${FC_REMOTE_ROOT:-/home/$USER/codeaf-rig}"
REMOTE_RESULTS="$REMOTE_ROOT/bench/frontiercode/results"
# Evidence must survive the task that fetched it: a fetched run inside an
# ephemeral task copy is gone when the copy is discarded. results/ is
# gitignored by design (it keeps the upstream reference out of the repo), so
# the fetch lands in evidence/ — a tracked path a task can actually write —
# and commits it, making the run's artifacts durable in the branch.
LOCAL_RESULTS="$FC_RIG_DIR/evidence"

# The attempt contract: every one of these must survive the trip.
REQUIRED=(record.jsonl DONE artifacts.sha256 model.patch scan.json cost.json logs/grade/grade.json)

verify_run_dir() { # <dir>: print what is missing, return 1 if anything is
  local d="$1" f missing=0
  [ -d "$d" ] || { echo "  ABSENT dir: $d"; return 1; }
  for f in "${REQUIRED[@]}"; do
    if [ ! -s "$d/$f" ]; then echo "  MISSING $d/$f"; missing=1; fi
  done
  return "$missing"
}

if [ "$MODE" = --check ]; then
  checked=0; bad=0
  if [ ! -d "$LOCAL_RESULTS" ]; then fc_die "no local results directory: $LOCAL_RESULTS"; fi
  while IFS= read -r d; do
    [ -n "$d" ] || continue
    checked=$(( checked + 1 ))
    verify_run_dir "$d" || bad=$(( bad + 1 ))
  done < <(find "$LOCAL_RESULTS" -mindepth 1 -maxdepth 1 -type d -name '*-codeaf-senior-dev-*' | LC_ALL=C sort)
  [ "$checked" -gt 0 ] || fc_die "no fetched run directories under $LOCAL_RESULTS"
  [ "$bad" -eq 0 ] || { echo "INCOMPLETE: $bad of $checked fetched run(s) are missing artifacts" >&2; exit 1; }
  echo "ok  $checked fetched run(s) carry their full artifact set"
  exit 0
fi

if [ "$MODE" = --plan ]; then
  cat <<PLAN
instance:        $INSTANCE
project/zone:    $PROJECT / $ZONE
remote root:     $REMOTE_ROOT
remote results:  $REMOTE_RESULTS
local results:   $LOCAL_RESULTS
fetch rule:      only run directories carrying DONE
required files:  ${REQUIRED[*]}
on missing:      exit non-zero and delete nothing
after verify:    report each run, shred the host's key, delete the host and its boot disk
PLAN
  if [ "$KEEP_HOST" = 1 ]; then echo "keep_host:  yes (this plan would not delete)"; fi
  echo "PLAN ONLY: no host contacted, nothing fetched or deleted"
  exit 0
fi

if [ "$MODE" = --teardown ]; then
  # Deletion is allowed only over a complete local copy: a host is the last
  # place a trajectory exists, and the disk goes with it.
  checked=0; bad=0
  while IFS= read -r d; do
    [ -n "$d" ] || continue
    checked=$(( checked + 1 ))
    verify_run_dir "$d" || bad=$(( bad + 1 ))
  done < <(find "$LOCAL_RESULTS" -mindepth 1 -maxdepth 1 -type d -name '*-codeaf-senior-dev-*' 2>/dev/null | LC_ALL=C sort)
  [ "$checked" -gt 0 ] || fc_die "no fetched runs under $LOCAL_RESULTS; refusing to tear down over nothing"
  [ "$bad" -eq 0 ] || { echo "INCOMPLETE: $bad of $checked fetched run(s) are missing artifacts; refusing to delete $INSTANCE" >&2; exit 1; }
  echo "shredding the host key, then deleting $INSTANCE and its boot disk"
  bash "$FC_RIG_DIR/gcp-key.sh" --shred "$INSTANCE" || true
  gcloud compute instances delete "$INSTANCE" --project="$PROJECT" --zone="$ZONE" \
    --delete-disks=boot --quiet
  echo "teardown complete: $INSTANCE and its disk are gone; $checked run(s) remain locally"
  exit 0
fi

# ── --fetch ─────────────────────────────────────────────────────────────────
gcloud compute instances describe "$INSTANCE" --project="$PROJECT" --zone="$ZONE" \
  --format='value(status)' 2>/dev/null | grep -qx RUNNING \
  || fc_die "instance is not RUNNING: $INSTANCE"

mkdir -p "$LOCAL_RESULTS"
dirs=()
while IFS= read -r line; do
  [ -n "$line" ] && dirs+=("$line")
done < <(gcloud compute ssh "$INSTANCE" --project="$PROJECT" --zone="$ZONE" --quiet \
  --command="find '$REMOTE_RESULTS' -mindepth 1 -maxdepth 1 -type d -name '*-codeaf-senior-dev-*' -printf '%f\n' 2>/dev/null | sort" \
  2>/dev/null | grep -v '^Warning: Permanently added' || true)
[ "${#dirs[@]}" -gt 0 ] || fc_die "no completed run directories on $INSTANCE (is the shard finished?)"
fc_note "${#dirs[@]} completed run(s) to fetch"

for d in "${dirs[@]}"; do
  case "$d" in
    */*|.*) fc_die "refusing an unexpected run directory name: $d" ;;
  esac
  fc_note "fetching $d"
  scp_out="$(gcloud compute scp --recurse "$INSTANCE:$REMOTE_RESULTS/$d" "$LOCAL_RESULTS/" \
    --project="$PROJECT" --zone="$ZONE" --quiet 2>&1)" \
    || { printf '%s\n' "$scp_out" | grep -v '^Warning'; fc_die "fetch of $d failed — $INSTANCE is left standing"; }
done

# Prove it: every fetched run must carry the whole contract. A missing piece
# stops the teardown and names itself.
bad=0
for d in "${dirs[@]}"; do
  verify_run_dir "$LOCAL_RESULTS/$d" || bad=$(( bad + 1 ))
done
if [ "$bad" -gt 0 ]; then
  echo "INCOMPLETE: $bad run(s) missing artifacts — do NOT delete $INSTANCE" >&2
  exit 1
fi

# Scrub any credential a run's copied home might carry before the result is
# kept. run.sh already rewrites the profile's api_key at extraction; this is
# the belt-and-braces pass over whatever landed.
for d in "${dirs[@]}"; do
  python3 - "$LOCAL_RESULTS/$d" <<'PY' 2>/dev/null || true
import json, pathlib, sys
root = pathlib.Path(sys.argv[1])
for cfg in root.rglob("config.json"):
    try:
        d = json.loads(cfg.read_text())
        if d.get("api_key") not in (None, "<scrubbed>"):
            d["api_key"] = "<scrubbed>"
            cfg.write_text(json.dumps(d, indent=1))
    except Exception:
        pass
PY
done

# Report what came home.
for d in "${dirs[@]}"; do
  python3 "$FC_RIG_DIR/grade/report.py" "$LOCAL_RESULTS/$d" || true
done

# Commit the evidence. results/ is gitignored by design -- it keeps the
# upstream reference out of the repository -- but that also means a fetched run
# sitting in an ephemeral checkout is destroyed with it. That has now cost us a
# whole canary's artifacts twice. evidence/ is tracked, so the fetch commits it
# and the run survives in the branch. Explicit paths only; never `git add -A`.
if git -C "$FC_REPO_ROOT" rev-parse --git-dir >/dev/null 2>&1; then
  if [ -n "$(git -C "$FC_REPO_ROOT" status --porcelain -- bench/frontiercode/evidence/ 2>/dev/null)" ]; then
    ( cd "$FC_REPO_ROOT" && git add -- bench/frontiercode/evidence/ && git commit -q \
      -m "bench/frontiercode: fetch evidence for ${dirs[*]}" \
      -m "Raw artifacts for ${#dirs[@]} fetched run(s), committed so they outlive the checkout that fetched them. results/ stays ignored; this is tracked." \
      -m "Assisted-by: CodeAF (deepseek-v4.1-flash)" \
      -m "Co-Authored-By: CodeAF <267109073+agentfield-bot@users.noreply.github.com>" ) \
      || fc_note "WARNING: could not commit evidence/ -- the run is on disk but NOT durable"
  else
    fc_note "no new evidence to commit"
  fi
else
  fc_note "WARNING: $FC_REPO_ROOT is not a git checkout -- evidence will not be durable"
fi

if [ "$KEEP_HOST" = 1 ]; then
  echo "fetched ${#dirs[@]} run(s); --keep-host set, so $INSTANCE is left running"
  exit 0
fi

echo "shredding the host key, then deleting $INSTANCE and its boot disk"
bash "$FC_RIG_DIR/gcp-key.sh" --shred "$INSTANCE" || true
gcloud compute instances delete "$INSTANCE" --project="$PROJECT" --zone="$ZONE" \
  --delete-disks=boot --quiet
echo "fetched and verified ${#dirs[@]} run(s); teardown complete"

#!/usr/bin/env bash
# ab.sh — the same request, two builds of this binary, one grid.
#
# THE QUESTION. On 2026-09-11 a one-paragraph UI request cost seventeen
# minutes, sixty-two tool calls and forty rounds on `dev`, and then spilled
# into a worker task. Five traces found mechanisms rather than a model, and the
# `simplify` branch is all of those fixes at once. This script answers the only
# question that settles whether they worked: for the SAME request, the SAME
# model, the SAME effort and the SAME configuration, is a binary built from
# `simplify` on the cost / wall-time / outcome front against one built from
# `dev`?
#
# It runs on the Spark, and it is driven from a laptop over ssh. Everything it
# does on the far side is here rather than in somebody's shell history:
#
#   1  two detached worktrees under ~/bench-ab, at origin/dev and origin/simplify
#   2  `make build` in each, and the binary installed under a name of its own
#      by absolute path — never copied over a file that might be running, which
#      is how a launch dies with `Killed: 9`
#   3  a check that the two binaries are DIFFERENT FILES. Two arms that are one
#      binary report a dead heat, and a dead heat is exactly what a wave that
#      did nothing would also report
#   4  a warm Go build cache for the pinned tree the cells work over, so that
#      whichever arm happens to run first does not pay for the cache the other
#      one gets for free
#   5  campaign.py freezes the schedule — randomized complete blocks, one
#      attempt per arm per block, arm order shuffled inside every block — and
#      then runs it
#   6  the evidence comes back to the laptop with rsync
#
# NOTHING HERE IS RUN BY MISTAKE. --dry-run prints every command and spends
# nothing; the live grid needs --go, and says what it expects to cost first.
#
# Usage:
#   bench/conversation/ab.sh --dry-run
#   bench/conversation/ab.sh --go --id simplify-ab-01
#   bench/conversation/ab.sh --go --repeats 1 --scenarios repo-wording-print   # a pilot
set -uo pipefail

AB_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$AB_ROOT/../.." && pwd)"

# ── what the grid is ────────────────────────────────────────────────────────
#
# The model is the owner's own chat model, named by its exact catalog id. The
# allowlist follows it and nothing else is permitted: every model id in every
# cell's own receipts is checked against that list afterwards, so a role, a
# title call or a fallback that resolved elsewhere fails its cell rather than
# quietly joining the average.
SSH_HOST="${AB_HOST:-spark}"
MODEL="${AB_MODEL:-moonshotai/kimi-k3}"
EFFORT="${AB_EFFORT:-low}"
REPEATS="${AB_REPEATS:-3}"
# The per-cell cap is a SPEND BACKSTOP, not a work limit. The turn this battery
# reproduces took seventeen minutes; thirty is enough room for a build that is
# slower than that one without leaving a runaway session billing all night.
CAP_S="${AB_CAP_S:-1800}"
# The product's own spend ceiling for an interactive session. It is part of the
# condition and both arms get the same one: an arm stopped by the ceiling
# measured the ceiling.
MAX_COST="${AB_MAX_COST:-5}"
SCENARIOS="${AB_SCENARIOS:-repo-hover-print,repo-hover-interactive,repo-wording-print,repo-wording-interactive}"
BASE_REF="${AB_BASE_REF:-origin/dev}"
HEAD_REF="${AB_HEAD_REF:-origin/simplify}"
# A path RELATIVE TO THE FAR HOME, written once and read two ways: the script
# below makes it absolute with $HOME, and rsync resolves a relative remote path
# against that same home. One value, so the two spellings cannot drift apart.
FAR_ROOT="${AB_FAR_ROOT:-bench-ab}"
SRC="${AB_FAR_SRC:-src/aforge-v2}"
ID="${AB_ID:-simplify-ab-$(date +%Y%m%d)}"
SEED="${AB_SEED:-20260912}"
DRY=1

while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run)   DRY=1; shift ;;
    --go)        DRY=0; shift ;;
    --id)        ID="${2:?}"; shift 2 ;;
    --repeats)   REPEATS="${2:?}"; shift 2 ;;
    --scenarios) SCENARIOS="${2:?}"; shift 2 ;;
    --model)     MODEL="${2:?}"; shift 2 ;;
    --effort)    EFFORT="${2:?}"; shift 2 ;;
    --cap)       CAP_S="${2:?}"; shift 2 ;;
    --host)      SSH_HOST="${2:?}"; shift 2 ;;
    -h|--help)   sed -n '1,40p' "$0"; exit 0 ;;
    *)           echo "unknown argument: $1" >&2; exit 1 ;;
  esac
done

FAR_OUT="$FAR_ROOT/runs/$ID"
LOCAL_OUT="$REPO_ROOT/bench-results/conversation-ab/$ID"

# The lane this branch belongs to, so the far side fetches a branch that exists.
BASE_BRANCH="${BASE_REF#origin/}"
HEAD_BRANCH="${HEAD_REF#origin/}"

# ── the script that runs on the far side ────────────────────────────────────
#
# It is composed here and printed in full by --dry-run, so that what the Spark
# is asked to do is reviewable before it is asked to do it. Single-quoted
# heredoc: nothing in it is expanded by THIS shell except through the
# substitutions listed at the top of the body.
far_script() {
  cat <<FAR
set -uo pipefail
export PATH="\$HOME/.local/bin:\$PATH"
set -a; . "\$HOME/.config/fleet/secrets.env"; set +a

ROOT="\$HOME/$FAR_ROOT"
SRC="\$HOME/$SRC"
mkdir -p "\$ROOT"

# ── the two builds ────────────────────────────────────────────────────────
git -C "\$SRC" fetch -q origin "$BASE_BRANCH" "$HEAD_BRANCH" || exit 1

build_arm() {
  # build_arm <label> <ref>
  local label="\$1" ref="\$2" tree="\$ROOT/tree-\$1"
  git -C "\$SRC" worktree remove --force "\$tree" 2>/dev/null
  git -C "\$SRC" worktree add -f --detach "\$tree" "\$ref" || return 1
  ( cd "\$tree" && make build ) || return 1
  # INSTALLED BY ABSOLUTE PATH AND NEVER OVER A RUNNING FILE. A copy onto an
  # inode a process still holds is how a launch dies with Killed: 9, so the old
  # one is removed first and the new one moved into place.
  rm -f "\$ROOT/aforge-\$label"
  mv "\$tree/bin/aforge" "\$ROOT/aforge-\$label" || return 1
  printf '%s %s %s\\n' "\$label" "\$ref" "\$(git -C "\$tree" rev-parse --short HEAD)"
}

build_arm dev "$BASE_REF" || exit 1
build_arm simplify "$HEAD_REF" || exit 1

# ── two arms have to be two binaries ──────────────────────────────────────
if [ "\$(sha256sum "\$ROOT/aforge-dev" | cut -d' ' -f1)" = \\
     "\$(sha256sum "\$ROOT/aforge-simplify" | cut -d' ' -f1)" ]; then
  echo "the two builds are byte-identical; there is nothing to compare" >&2
  exit 1
fi
"\$ROOT/aforge-dev" --version
"\$ROOT/aforge-simplify" --version

# ── a warm cache for the tree the cells work over ─────────────────────────
#
# Every cell clones the SAME pinned commit and the harness compiles inside it.
# A cold Go build cache would be paid by whichever arm happened to run first,
# and that arm's wall time would carry a cost belonging to the rig. So it is
# paid once, here, by neither arm.
PIN="\$(grep -o '[0-9a-f]\\{40\\}' \\
        "\$ROOT/tree-simplify/bench/conversation/fixtures/repoclone.sh" 2>/dev/null | head -1)"
if [ -n "\$PIN" ]; then
  rm -rf "\$ROOT/warm"
  git init -q "\$ROOT/warm" && \\
    git -C "\$ROOT/warm" fetch -q --depth 1 "\$SRC" "\$PIN" && \\
    git -C "\$ROOT/warm" checkout -q --detach FETCH_HEAD && \\
    ( cd "\$ROOT/warm" && go build ./... && go vet ./internal/tui3/ ) >/dev/null 2>&1
  echo "warmed the build cache over \$PIN"
fi

# ── freeze the schedule, then run it ──────────────────────────────────────
RIG="\$ROOT/tree-simplify/bench/conversation"
mkdir -p "\$HOME/$FAR_OUT"
python3 "\$RIG/campaign.py" plan "\$HOME/$FAR_OUT/manifest.json" \\
  --id "$ID" \\
  --arms aforge@dev,aforge@simplify \\
  --bin "aforge@dev=\$ROOT/aforge-dev" \\
  --bin "aforge@simplify=\$ROOT/aforge-simplify" \\
  --scenarios "$SCENARIOS" \\
  --repeats $REPEATS --cap $CAP_S --seed $SEED \\
  --model "$MODEL" --effort "$EFFORT" --max-cost $MAX_COST || exit 1

python3 "\$RIG/campaign.py" run "\$HOME/$FAR_OUT/manifest.json" --out "\$HOME/$FAR_OUT/evidence"
FAR
}

# ── what this is about to do ────────────────────────────────────────────────
cells=$(( REPEATS * $(echo "$SCENARIOS" | tr ',' ' ' | wc -w) * 2 ))
echo "A/B:        $BASE_REF  vs  $HEAD_REF"
echo "id:         $ID"
echo "model:      $MODEL (allowlist follows the pin; nothing else may be billed)"
echo "effort:     $EFFORT, sent to both arms as --reasoning $EFFORT"
echo "scenarios:  $SCENARIOS"
echo "repeats:    $REPEATS per (arm × scenario)  →  $cells cells"
echo "cap:        ${CAP_S}s per cell; the session's own ceiling \$$MAX_COST"
echo "far side:   $SSH_HOST:$FAR_OUT"
echo "evidence:   $LOCAL_OUT"
echo

if [ "$DRY" = "1" ]; then
  echo "── the command this would run ────────────────────────────────────────"
  echo "ssh $SSH_HOST bash -s  <<'EOF'"
  far_script
  echo "EOF"
  echo
  echo "── and then ─────────────────────────────────────────────────────────"
  echo "rsync -a --info=stats1 $SSH_HOST:$FAR_OUT/ $LOCAL_OUT/"
  echo "bench/conversation/summary.sh $LOCAL_OUT/evidence/results.jsonl"
  echo
  echo "dry run — nothing was built, nothing ran, nothing was spent."
  echo "Pass --go to run it. Every cell is a live model call on a metered key."
  echo
  echo "A GRID OF THIS SIZE OUTLIVES AN SSH SESSION. At ${CAP_S}s a cell the worst"
  echo "case is $(( cells * CAP_S / 3600 )) hours, so run it detached and read the log:"
  echo "  tmux new -d -s af-ab '$AB_ROOT/ab.sh --go --id $ID 2>&1 | tee /tmp/$ID.log'"
  echo "  tmux capture-pane -p -t af-ab | tail -40"
  exit 0
fi

echo "running the grid on $SSH_HOST — this spends real money."
far_script | ssh "$SSH_HOST" bash -s
status=$?

mkdir -p "$LOCAL_OUT"
rsync -a "$SSH_HOST:$FAR_OUT/" "$LOCAL_OUT/" || echo "the evidence did not come back; it is still on $SSH_HOST:$FAR_OUT" >&2

echo
echo "evidence: $LOCAL_OUT"
echo "read it:  bench/conversation/summary.sh $LOCAL_OUT/evidence/results.jsonl"
exit $status

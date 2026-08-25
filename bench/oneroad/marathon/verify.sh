#!/usr/bin/env bash
# marathon/verify.sh — the dataset's own verdict, run inside the task container.
#
# UNLIKE THE SWE TRACK, THIS RUNS tests/test.sh WHOLE. The SWE verifier had to be
# taken apart because three of its five stages are LLM judges we have no key for.
# Marathon's is entirely deterministic: it rebuilds the agent's crate, scans the
# source for cheats, hashes the corpus against a pristine manifest, decrypts the
# golden with a key embedded in the script itself, scores the visible corpus and
# the holdout, and writes reward.txt and metrics.json. Nothing here needs a model,
# so nothing here is skipped and the verdict is the benchmark's own, unedited.
#
# This script only does the three things test.sh cannot do for itself: move the
# AGENT's leavings out of /logs first, keep the main corpus's per-method table
# before the holdout merge overwrites it, and record what the workspace looked
# like at the moment of judgement.
set -uo pipefail
WORKDIR="${WORKDIR:-/workspace/rust-java-lsp}"

# ── the agent's own score is not the verifier's ─────────────────────────────
# The image ships run_tests.sh, and an agent that ran it wrote its OWN
# reward.txt and metrics.json into /logs/verifier against the VISIBLE corpus.
# Left in place, test.sh would overwrite some of those files and leave others,
# and the record would carry a metrics.json half written by the thing being
# measured. So the agent's phase is moved aside — kept, because what the agent
# believed about its own progress is evidence, just not the verdict.
if [ -d /logs/verifier ] || [ -d /logs/artifacts ]; then
  mkdir -p /logs/agent-phase
  mv /logs/verifier /logs/agent-phase/verifier 2>/dev/null
  mv /logs/artifacts /logs/agent-phase/artifacts 2>/dev/null
fi
mkdir -p /logs/verifier

# ── what was in the workspace at the moment of judgement ────────────────────
# The image ships exactly one file in the working directory (run_tests.sh), so
# this count is how the record tells "the agent landed something" from "the agent
# landed nothing" without having to read a diff that does not exist — this task
# has no git repository to diff against.
find "$WORKDIR" -type f -not -path '*/target/*' -not -path '*/.git/*' \
  | head -5000 | sort > /logs/oneroad_workspace.txt 2>/dev/null
wc -l < /logs/oneroad_workspace.txt > /logs/oneroad_workspace_count.txt

# ── keep the main corpus's per-method table ─────────────────────────────────
# score_golden.py writes partial_score, pass_rate, passed, total AND per_method
# to /logs/verifier/metrics.json — and then test.sh's holdout merge REPLACES that
# file with a summary that has no per_method in it at all. The per-method
# breakdown is the most useful thing the verifier produces (it is what says
# whether an attempt got hover right and references wrong), so the first complete
# metrics.json is copied aside the moment it appears. `cp -n` means the first
# writing wins; the json.loads guard means a half-written file is not the one
# that wins.
python3 - <<'PY' &
import json, os, time
src = "/logs/verifier/metrics.json"
dst = "/logs/verifier/metrics_main.json"
deadline = time.time() + 4 * 3600
while time.time() < deadline:
    if os.path.exists(dst):
        break
    try:
        d = json.load(open(src))
        if "per_method" in d:
            json.dump(d, open(dst, "w"), indent=2)
            break
    except Exception:
        pass
    # A QUARTER OF A SECOND, BECAUSE TWO SECONDS WAS ALREADY TOO SLOW. A server
    # that answers instantly scores all 68,186 points in about five seconds, and
    # on the s1 aforge cell the gap between the main metrics.json appearing and
    # the holdout merge replacing it was under a second — the two-second poll
    # missed it and the per-method table had to be recovered from the printed
    # log. The fallback works; the file is the better source.
    time.sleep(0.25)
PY
SNAPPER=$!

# ── the verifier, whole ─────────────────────────────────────────────────────
echo "== tests/test.sh ==" >&2
bash /tests/test.sh
echo "test_sh_exit=$?" >&2
kill "$SNAPPER" 2>/dev/null

# What was and was not asked, written beside the results so a reader of the row
# never has to reconstruct it from this file.
cat > /logs/verifier/oneroad_stages.json <<JSON
{
  "ran": ["tests/test.sh (whole: cargo build, anti_cheat, verify_integrity, cached-golden scan, score_golden main, score_golden holdout, merge)"],
  "skipped": [],
  "deviations": [
    "network: the container runs on docker's default bridge with full egress, not the task's crates.io+model allowlist — no egress allowlist was built for this run",
    "storage: task.toml asks for a 20480 MB quota; docker's overlay2 here enforces no per-container disk quota"
  ]
}
JSON
exit 0

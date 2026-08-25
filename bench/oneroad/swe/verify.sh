#!/usr/bin/env bash
# verify.sh — the dataset's verdict, minus the stages we cannot honestly run.
#
# It runs INSIDE the task container and is deliberately NOT `tests/test.sh`.
# test.sh has five stages and three of them are LLM judges (rubric, taste) or a
# validation agent (mini-swe-agent), all of which want ANTHROPIC/OPENAI keys we
# do not provide. Running test.sh whole would spend minutes installing litellm
# and mini-swe-agent to produce judge outputs that fail, and — worse — it would
# leave a reward file assembled from failed judges that reads like a score.
#
# So this runs exactly the stages that are DETERMINISTIC and says which it
# skipped, in a file the row records:
#
#   stage 0  capture the agent diff        — replicated from test.sh verbatim
#   setup    tests/test-setup.sh           — services and toolchain the verifiers need
#   stage 1  run_verify.py                 — the native verifiers. THE VERDICT.
#   stage 5  run_aggregate.py aggregate    — assembles reward.json from what exists
#
#   SKIPPED: stage 2 rubric judge, 2b rubric-all, 3 taste judge, 4 validation agent.
#
# A VALIDATION-PRIMARY TASK HAS NO VALID REWARD HERE and the aggregator says so
# itself: with a validation_spec.toml present and no validation_results.json, it
# refuses to fall back to verifier-only scoring (run_aggregate.py's comment C2).
# That refusal is correct and is recorded as "reward unavailable", never as 0.
set -uo pipefail
REPO_NAME="${REPO_NAME:?verify.sh needs REPO_NAME}"
mkdir -p /logs/verifier

_SYS_PYTHON="$(readlink -f "$(command -v python3)")"
export _SYS_PYTHON

# ── stage 0: the agent diff, before anything else can touch /repo ───────────
# Replicated from tests/test.sh so the artifact a judge reads is the artifact
# the dataset defines: base checkpoint..agent commit, hooks disabled so a
# repo-installed pre-commit hook cannot abort the commit and leave it empty.
_BASE_REF=/var/lib/devcontainer_base_ref
_REPO="/repo/$REPO_NAME"
if [ -f "$_BASE_REF" ] && [ -d "$_REPO/.git" ]; then
  _BASE=$(tr -d '[:space:]' < "$_BASE_REF")
  (
    cd "$_REPO"
    git config user.email automation@localhost 2>/dev/null
    git config user.name devcontainer 2>/dev/null
    git add -A 2>/dev/null
    git diff --cached --quiet || \
      git -c core.hooksPath=/dev/null commit -m "agent changes" --no-gpg-sign --no-verify 2>/dev/null
    git rev-parse HEAD > /var/lib/agent_ref 2>/dev/null
    git diff "$_BASE"..HEAD > /logs/verifier/agent.patch 2>/dev/null
  ) || true
fi

# ── setup, then the native verifiers ───────────────────────────────────────
# test-setup.sh is SOURCED, exactly as test.sh sources it: it exports variables
# and may activate a project venv that the verifiers then run under.
echo "== test-setup.sh ==" >&2
set +e
source /tests/test-setup.sh
echo "== run_verify.py ==" >&2
"$_SYS_PYTHON" /tests/run_verify.py
echo "verify_exit=$?" >&2

echo "== run_aggregate.py aggregate ==" >&2
"$_SYS_PYTHON" /tests/run_aggregate.py aggregate
echo "aggregate_exit=$?" >&2

# What was and was not asked, written beside the results so a reader of the row
# never has to reconstruct it from this file.
cat > /logs/verifier/oneroad_stages.json <<JSON
{
  "ran": ["agent_diff", "test-setup", "run_verify", "run_aggregate"],
  "skipped": ["run_judge rubric", "run_judge rubric-all", "run_judge taste", "run_validate"],
  "skipped_because": "no ANTHROPIC/OPENAI/PORTKEY key is provided to this benchmark; an LLM judge that cannot call a model returns a failure that would otherwise be aggregated as if it were a score",
  "validation_primary": $([ -f /tests/validate/validation_spec.toml ] && echo true || echo false)
}
JSON
exit 0

# PLAN-pilot.md — the first FrontierCode-style pilot (D4)

*A pilot plan for the owner to approve. Nothing here has run yet beyond the
D2 slice. The brief's cap: under $20 total on pilots; any campaign beyond the
pilot waits for approval (brief §5, §10).*

## What a pilot must answer

One question, the one the whole rig exists for: **does senior-dev produce
work a maintainer would merge more often than a plain loop agent on the same
model?** DeepSWE answered "do hidden tests pass"; FrontierCode's conditions
add test quality, scope, style and conventions — exactly where a
harness-tuned-for-hidden-tests may not transfer. The pilot measures that
transfer on a small, honest task set.

## Task set: 6–8 tasks

- **The calibration fixture** (`jsonschema-log-warning`) — C++, already
  built, already calibrated against the labelled example (gold 1.00, negative
  0.0 with both blockers failed).
- **5–7 more, mined with the pipeline below**, one per repository,
  preferring languages the models see least in SWE-bench-style corpora
  (C++, Rust, Go, shell-heavy Python tooling) — the brief notes
  FrontierCode covers three times as many languages as SWE-Bench Pro.
- **A dev/held-out split from the first task written** (the minimal rig's
  rule): the pilot runs on the dev half; the held-out half exists so a pilot
  gain can be checked against tasks nobody tuned on.

### Sourcing sketch (drafted, not finished — brief §4.6)

1. **Find merged PRs** in repositories with real linters and conventions
   (clang-format/ESLint/ruff configs present in the tree), merged AFTER the
   candidate models' training cutoffs, that touch code rather than docs or
   CI. Source of candidates: the repo's own git log on a full clone,
   filtered by files-touched and size.
2. **Write the brief** in the four FrontierCode sections, guideline text
   taken from the repository's own CONTRIBUTING files and CI config —
   never invented.
3. **Draft the rubric** from the reference PR: 6–12 criteria, kinds chosen
   from the six, blockers marked where the PR would not have merged without
   it. Have a model draft them, then a person review every one — Cognition's
   own account says their rubrics needed adversarial QC (75 overly strict
   blockers demoted in their 1.1 audit); ours will need the same scrutiny.
4. **Build the fixture**: environment image (pinned base commit, hygiene
   assertions), verifier image (reference patch + overlay + controls), gold
   control MUST score 1.00 and the negative control (where one can be
   generated) MUST score 0.0 with its blockers failed before the task is
   usable.
5. **Record the split**: `tasks/dev.txt`, `tasks/heldout.txt`, committed.

## Arms and model

- **Arms**: `codeaf-senior-dev`, and `mini-swe-agent` through Pier (the D3
  arm, same rig, same grader). `codex` optional when its OpenRouter routing
  is settled.
- **Model**: `deepseek/deepseek-v4-flash-0731` — the model the DeepSWE
  campaigns pinned, so pilot results cross-check against that history (brief
  §6). One model for the whole pilot; a second model (Kimi K3) only if the
  owner asks.
- **Seeds**: 2 per arm per task for the pilot (5 for anything quoted
  publicly). Variant `high`, recorded per row.

## Protocol

- Every arm, every seed: same task, same model, same rubric, same grader,
  same 4 CPU / 8 GiB container budget, same wall (2 h) and cost cap ($5).
- Scanner on every agent run; a flagged run scores 0 and counts toward flag
  rate.
- Compare tasks in PAIRS across arms; report the per-task win/loss column,
  not just the mean. The table is `grade/report.py`'s.
- **Preregistration** (drafted below) before the first pilot run.

## Cost estimate

| item | figure | basis |
|---|---|---|
| gold + negative controls per task | $0 (CPU only) + ~$0.03 judge | measured on the fixture |
| senior-dev rollout | ~$0.3–1.5 | DeepSWE full113 history + this task's C++ build cost |
| mini-swe-agent rollout | ~$0.1–0.5 | fewer turns, smaller context |
| judge per grade | ~$0.02–0.05 | measured (~$0.006/criterion call) |
| **pilot total** | **8 tasks × 2 arms × 2 seeds ≈ 32 rollouts ≈ $8–16** | within the $20 cap |

All spend goes through the credential guard (per-call usage rows) — no
account-balance arithmetic anywhere.

## Preregistration draft (write the full one before the first run)

- **Hypothesis**: senior-dev's pass rate (cleared every blocker, unflagged)
  on the dev tasks is at least mini-swe-agent's, at comparable or lower cost
  per rollout, on the same model.
- **Arms**: the two above, same model, 2 seeds each.
- **Metric**: pass rate; secondary: mean score, flag rate, cost per rollout,
  wall seconds.
- **Analysis**: per-task paired comparison; no pooling across the dev/held-out
  line; wall-clock compared only within the same host architecture.
- **Stopping**: when every planned cell has a grade (or rig), or when the $20
  cap is reached — whichever first. A rig-failed cell is re-run once; twice is
  a rig bug to fix first.
- **Known threats**: the corpus is ours, so no leaderboard comparison is
  implied; judge quality is calibrated only on the fixture's controls; 2
  seeds cannot resolve small differences.

## What blocks the pilot

1. **Owner questions** (brief §10): budget ceiling beyond $20, cloud for
   scale-out (GCP/RunPod/Modal) vs local Colima, model set confirmation, and
   whether to approach Cognition.
2. **Task sourcing is hand-work**: each task needs a person-reviewed rubric;
   the pipeline drafts, a person approves. Budget ~1 hour per task.
3. **The judge needs adversarial QC** like Cognition's: the fixture's
   controls calibrate it; every new task's rubric should run gold + a
   hand-made negative before use.
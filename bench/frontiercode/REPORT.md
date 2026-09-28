# Final report — the FrontierCode-style rig

Written 2026-09-28 against brief §12's six points. Everything below is about
`bench/frontiercode/` at commit `b61a1c5d8` (branch `task/build-frontiercode-style-bench-r-18f40e`,
**not pushed** — per the brief, no push, no PR; the head hash is the hand-over).

## 1. What works, with evidence

- **The rig runs and grades.** `run.sh` drives `codeaf senior-dev` (built with
  `make build`) inside a container-in-container run: credential guard (holds the
  key, meters every call), open-and-logged egress proxy, agent container, then
  `grade.sh` grades the collected diff against the task's twelve-criterion
  rubric — prompt criteria judged by an LLM judge on the host
  (openrouter/anthropic/claude-sonnet-4.5, prompt id `fc-judge-1`), command and
  classical criteria run offline in a `--network none` verifier.
- **Acceptance (a) — gold 1.00**: `results/jsonschema-log-warning-gold/`
  (`logs/grade/grade.json`: score 1.0, no failed blockers; phase A 9 pass /
  0 fail / 0 rig).
- **Acceptance (b) — negative 0.0**: `results/jsonschema-log-warning-negative/`;
  both prompt blockers fail with judge reasoning naming the leftover
  `std::cerr` lines; the 10 non-blockers still pass, as intended.
- **Acceptance (c) — sealed fixture flagged**: `results/jsonschema-log-warning-seal/`;
  the scanner's hard reasons include the upstream slug in the transcript and the
  `raw.githubusercontent.com` connection through the proxy.
- **Acceptance (d) — a live rollout, complete row**: the senior-dev arm on
  `deepseek/deepseek-v4-flash-0731` scored **1.00** (all twelve criteria pass),
  $0.044 harness-metered and guard-metered, 1141 s wall, egress unflagged —
  `results/jsonschema-log-warning-codeaf-senior-dev-deepseek-deepseek-v4-flash-0731-s3/`.
  Two earlier rolls (s1 $0.071, s2 $0.116) also scored 1.00 and are retained in
  the append-only records.
- **The D3 baseline arm ran for real**: mini-swe-agent via Pier on the same
  fixture, the same model, graded by the same grader — **1.00** at $0.0516,
  21.5 min (`results/jsonschema-log-warning-mini-swe-agent-pier-...-s1/`,
  design notes in `PIER-BASELINE.md`). Both arms solved the fixture; the
  scoreboard's calibration rows (gold 1.00, negative 0.00, seal flagged) make
  the comparison meaningful.
- **The environment image runs the project's own full suite clean** (301 tests,
  exit 0) — which the first two rolls proved is load-bearing (finding in §4).
- **Spent**: ≈ $0.31 total (four rollouts, judge calls, one probe) — the brief's
  $20 cap was never approached; all compute was local Colima.

## 2. What is stubbed or not yet done

- **Adaptive classical** (§5 of README) is implemented and machinery-tested but
  no live roll has yet taken the adaptation branch.
- **Registry-install version comparison** in the scanner: installs are recorded,
  versions are not yet diffed against the base commit's pins.
- **The corpus is one task.** Task sourcing is a drafted pipeline
  (`PLAN-pilot.md`), not a corpus. The pilot needs the fixture-building to be
  run per task (an afternoon of work per task as drafted).
- **Path-level (MITM) egress logging** is by design out (hostname +
  transcript only) — a documented choice, not a stub.
- **One seed per arm** — the pilot plan sets the seed budget; today's rows are
  single-seed.

## 3. Deviations from the brief

- **D1 location decision followed the brief's default** (`bench/frontiercode/`
  in this repo); the minimal-rig repo was used as a reference library only.
- **Pier baseline arm's egress posture is Pier's** (allowlist to
  `openrouter.ai`), stricter than FrontierCode's open internet — recorded in
  the row and in `PIER-BASELINE.md`. A cross-arm task requiring web access
  would not be comparable in this posture; PLAN-pilot.md notes the choice.
- **The senior-dev binary is `dirty`-tagged** (built from an uncommitted tree
  mid-session) for the recorded rolls; the rig records `rig_rev` per run.
- **Two arms shared one result directory on the first day** (the name lacked
  the arm) — fixed in the same change (arm now in the name; `pier-arm.sh`
  refuses an existing directory), with the collision's evidence split and the
  senior-dev row regenerated clean (s3).

## 4. What the first day surfaced

- **`xxd` missing from the environment image** made three base-tree tests fail,
  which made two senior-dev rolls terminate `fail` ("submitted a change that
  the project's own build or tests do not pass") on changes the rubric scored
  1.00 twice. The image installs `xxd` now; the next roll finished with the
  harness's own verification passing. Lesson: the environment must run the
  project's own full suite clean, because the harness under test may pin
  exactly that command.
- **senior-dev on this fixture is fast and cheap**: three rolls solved the
  task in 6–19 min at $0.04–0.12 on a small open-weights model.

## 5. Open owner questions (brief §10, untouched)

- Cloud budget for the pilot (today's rows ran on local Colima; a 5–10 task
  pilot fits the $20 cap at open-model prices, README §9).
- The model set for the pilot (today: `deepseek/deepseek-v4-flash-0731`).
- Whether to approach Cognition — the brief's paraphrase-only rule is applied
  throughout (README §8); approaching is the owner's call.

## 6. Risks

- **Judge dependence**: prompt-blocker grading rests on one judge model and
  prompt (`fc-judge-1`); the controls catch calibration drift but a judge
  model change invalidates cross-run comparisons. PLAN-pilot.md pins the judge
  per campaign and re-runs controls under it.
- **One fixture**: every generalization from today is about this task.
- **macOS law-suite noise on this laptop** (unrelated to this work):
  `make test-laws` on the main tree is red from other lanes' `.claude/worktrees/*`
  being walked; in an isolated worktree at the hand-over commit the only red
  is `internal/session`'s three job-log tests failing on the macOS `/private`
  temp-dir symlink prefix — pre-existing, untouched by this change.

## 7. Suggested next step

Run the pilot's first tranche exactly as preregistered in `PLAN-pilot.md`:
three more fixtures sourced by its pipeline, the same two arms and controls,
and the seed budget set there — the rig, its controls and the scoreboard are
ready for that the moment the owner names the model set and the cloud budget.
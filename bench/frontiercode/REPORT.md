# Final report — the FrontierCode-style rig

Written 2026-09-28 against brief §8's six points (the list of what the report
must carry). Everything below is about
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
## First GCP canary (2026-09-29)

- **Host:** `fc-pilot-s1`, `openaf-505800` (the GCP project's identifier), `us-central1-a`, e2-standard-8, external 35.192.51.155, `--max-run-duration 6h --instance-termination-action=STOP`. Branch `bench/frontiercode` tip 6c7253696 plus three host-path fixes committed during the run (see "What broke"). <!-- legacy-name -->
- **Controls (host path, `launch.sh --controls 1`):** gold jsonschema-log-warning **1.00**; negative **0.0** with **both blockers failed**; seal **FLAGGED** (planted leak, score 0). All three reproduced — `ok controls on fc-pilot-s1`.
- **Rollout (`launch.sh --execute 1`):** one task, one seed, one wave. **Score 1.00, pass**, no blockers failed. Run dir `results/jsonschema-log-warning-codeaf-senior-dev-deepseek-deepseek-v4.1-flash-s1` (grade.json, record row, scan.json, cost.json, guard-usage.jsonl, DONE sentinel all present and fetched with a verified artifact set).
- **Cost:** harness-reported **$0.0511**; guard-metered **$0.0511** (51 calls, 1.74M prompt / 26.2k completion tokens, `guard_unknown_cost_calls 0`). Judge spend (sonnet-4.5) included in the key delta.
- **Key usage delta (frontiercode-generic):** baseline at install **$0.000** → final **$0.0985** (covers rollout + three controls' grading). Daily limit $1000, untouched.
- **Wall time:** create → teardown ≈ 3h45m (dominated by staging fixes and image warm); rollout itself 14:29:55Z→14:35:46Z (5m51s); controls ≈ 3 min.
- **What broke (fixed on the branch, each called out):**
  1. `gcp-stage.sh` line 60: `git for-each-ref | head -1` died of SIGPIPE under `set -o pipefail` (exit 141, no output) — replaced with `awk 'NR==1{print}'`.
  2. `gcp-stage.sh`: the bundle named only `refs/heads/<branch>`, so the host's `git fetch` could not resolve HEAD ("couldn't find remote ref HEAD") — bundle now created with `HEAD` included.
  3. `gcp-stage.sh`: `bin/` is gitignored so it is not in the bundle; `install` target dir missing on host — `install -D`.
  4. `lib.sh`: `ensure_env_image`/`ensure_verify_image` log under `results/` before it exists, so the first warm loop silently failed ("warm: environment image failed") — both now `mkdir -p "$RESULTS"`.
  Also: `--check-local` initially failed because the gitignored pinned binaries were absent in the fresh tree (copied from the owner checkout, sha256 verified `fde4a42d…`).
- **Teardown:** key shredded, instance and boot disk deleted; `gcloud compute instances list --project openaf-505800` (the project id) lists 0 items, no orphan disk. Nothing bills after the run. <!-- legacy-name -->

## Second GCP canary (5-trial)

- **Host:** `fc-pilot-s1`, `openaf-505800` (the GCP project's identifier), `us-central1-a`, e2-standard-8, 120 GB boot disk, 6h self-stop. Branch `bench/frontiercode` tip `5cfd4a924`; one host-path fix committed during the run (see "What broke"). First exercise of the official 5-trial protocol: manifest `seed_ids: [s1..s5]`, single reasoning effort `high`, one host. <!-- legacy-name -->
- **Controls (host path, `launch.sh --controls 1`):** gold jsonschema-log-warning **1.00**; negative **0.0 with both blockers failed**; seal **FLAGGED** (planted leak, score 0). All three reproduced — `ok controls on fc-pilot-s1`.
- **Rollout (`launch.sh --execute 1`):** 5 trials, one per seed s1–s5, one effort, one wave. Labels `fc-pilot-shard1-ehigh-s<seed>-wave1`.

| seed | score | pass | flagged | blockers fail | criteria | out tok | cost$ (harness) | wall (s) |
|---|---|---|---|---|---|---|---|---|
| s1 | 1.00 | yes* | **yes\*** | none | 12/12 | 45341 | 0.050 | 522 |
| s2 | 1.00 | yes | no | none | 12/12 | 20367 | 0.054 | 353 |
| s3 | 1.00 | yes | no | none | 12/12 | 46509 | 0.090 | 477 |
| s4 | 1.00 | yes | no | none | 12/12 | 25589 | 0.047 | 357 |
| s5 | 1.00 | yes | no | none | 12/12 | 20923 | 0.027 | 345 |

\* s1: the judge graded 1.00 / pass, 12/12 criteria, zero failed blockers; the seal scanner FLAGGED it, which zeroes it to 0.0/pass. This is a **false positive** — see "What broke" #2. Report table below therefore counts s1 as flagged.

`grade/report.py` aggregate (over the 5 run dirs):

```
  EFFORT TRIALS GRADED  PASS MEAN SCORE MEAN OUT TOK
----------------------------------------------------
    high      5      5     4     0.8000        31746
best-performing reasoning effort: high (mean score 0.8000)

runs 5: 4 pass, 1 flagged, 5 graded, 0 rig; total $0.268 (harness-reported)
```

- **Cost:** harness-reported **$0.268** (5 rollouts, ~$0.05 each); guard-metered matches. Judge spend (sonnet-4.5, 3 controls) included in the key delta.
- **Key usage delta (frontiercode-generic):** baseline (provider-side usage at install) **$0.098469** → final (at teardown) **$0.469695** → **delta $0.37123**. Daily limit $1000, $0.0985 used by campaign start.
- **Wall time:** create → teardown ≈ 3h48m. Controls ≈ 3 min. Rollout wave 15:58Z→16:42Z (≈ 44 min, ~8–9 min/trial). Staging/warm ≈ 20 min.
- **What broke:**
  1. **`gcp-create.sh` plan gate** failed on the aligned manifest: it required the old scalar `.seed_id` (`fc_require_keys .seed_id`, printed `fc_get '.seed_id'`), but the manifest now carries `seed_ids` (array) — null `.seed_id`. Fixed to `fc_require_keys` without `.seed_id` and print `fc_seeds` (the same fallback gcp-lib/launch/fetch already use). One-line, called out.
  2. **Seal-scanner false positive on s1.** The agent worked in the task's own repo (`~/repos/jsonschema`, as its instructions say), running `cd ~/repos/jsonschema && grep … src/error.h`. `src/error.h` embeds the repo's own issues URL `https://github.com/sourcemeta/jsonschema`. The scanner treats that as an upstream-slug leak and flags the run → zeroed to 0.0. The egress proxy log shows only model-plane hosts (codeaf.agentfield.ai, models.dev) — no external fetch; judge already scored 1.00 with all criteria met. The flagged text is the task's own source and path. Not patched — reported per policy (do not change rig scripts to make a failure go away); recommend the scanner exclude the run's own task repository path/slug when the URL only appears inside source fetched by the task itself.
- **Teardown:** key shredded (`--shred fc-pilot-s1`), instance and boot disk deleted (`gcloud compute instances delete fc-pilot-s1 --delete-disks=all`); `gcloud compute instances list --project openaf-505800` (the project id) lists 0 items, no orphan disk. Nothing bills after the run. <!-- legacy-name -->
- **Artifacts fetched home:** `bench/frontiercode/results/jsonschema-log-warning-codeaf-senior-dev-deepseek-deepseek-v4.1-flash-s1..s5-ehigh/`, each with grade.json, scan.json, record.jsonl, cost.json, meta.json and DONE sentinel, verified by `fetch-results.sh` (`fetched 5 run(s); --keep-host set`). Raw `--controls` and `--execute` transcripts preserved in this run's task log and `fc-controls.txt`.

## Plan steps 1–4 status (2026-09-29, branch bench/frontiercode)

1. **D4 merge — done.** The three conflicts were union-merged and committed
   (the changelog resolution completed in `0f3e86d84`); gates green
   (namelaw — after marking the GCP project id in this report with the
   sanctioned `legacy-name` marker —, changes check, script syntax).
2. **Candidate environments — 2 of 7 built and calibrated.**
   `shell-pipe-empty-command` (goreleaser, Go) and
   `conflicted-files-refname-crash` (pre-commit, Python) each have a full
   task directory, built environment and verifier images, and reproduce both
   controls: gold 1.00, negative 0.0 with both blockers failed. Building them
   surfaced and fixed four rig-generalization bugs (grade.sh repo path and
   mountless phase execution, judge reply parsing and adapt-shape, per-task
   reverse rebuild, generic adaptive trigger); the fixture's gold control was
   re-run under the changed grader and still scores 1.00. The remaining five
   candidates (kong, cobra, headscale, bubbletea, clap) are unstarted; their
   PR head/base commits are verified and recorded above, and the pipeline is
   now mechanical.
3. **Scanner false positive — fixed.** A bare upstream-slug mention in the
   transcript is a SOFT note unless there is fetch evidence in the line
   (clone/fetch command, `.git` URL, upstream-history or archive URL shape)
   or any github-family egress in the proxy log. Four probe cases verified:
   the s1 shape (slug inside source, clean proxy log) no longer flags; a
   clone command, slug-plus-github-egress, and a PR URL still hard-flag.
4. **Full benchmark run — staged, not launched.** The manifest carries the
   agreed design (5 seeds in one wave, 4 CPU/8 GiB per container,
   `e2-standard-32`, 120 GB, 6h self-stop); `launch.sh --check-local` and
   `gcp-create.sh --plan` both pass with the three calibrated tasks in
   shard-1. The run itself needs a supervised ~6h window and mandatory
   teardown, which this task's budget could not guarantee, so it was not
   started; launch is `gcp-stage.sh` → `gcp-key.sh` → `gcp-create.sh` →
   `launch.sh` → `fetch-results.sh` → teardown.

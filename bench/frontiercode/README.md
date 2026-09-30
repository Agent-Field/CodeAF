# The FrontierCode rig — a "would a maintainer merge this" grader for our harness

*Built 2026-09-28. The brief this rig implements is the owner's FrontierCode
harness work order (kept off the repository, on the owner's desktop). The
numbers this rig produces are for our own harness development only: they are
not Cognition's FrontierCode scores, they are not comparable to its
leaderboard, and they must never be presented as such.*

FrontierCode (Cognition, June 2026) grades whether a maintainer would *merge*
a change — tests, test quality, scope, style and project conventions — where
DeepSWE only grades whether hidden tests pass. Its tasks are private and are
not being released. This rig reproduces the conditions we can, on our own
tasks, and grades with the six criterion kinds its leaderboard documents.

Everything here was built to be re-checked: every upstream byte lives in a
container image fetched from a pinned commit, every control is a script, and
every run leaves an append-only record whose fields are the report's columns.

## 1. Which base, and why (the brief's D1 question)

**Chosen: `bench/frontiercode/` in the codeaf repository, reusing
`bench/deepswe/` code and importing disciplines from the minimal rig
(`~/Code/swe-pro-go-deepswe-minimal`).** Not the minimal rig, not a fork of
either.

Why this base:

- **The harness under test builds here.** senior-dev ships inside codeaf;
  `make build` produces `bin/codeaf`, and the rig copies that binary into the
  task container, exactly as `bench/deepswe/run.sh` does. A rig outside the
  repository would have to reach across checkouts for every run.
- **`bench/deepswe/` carries the pieces that were paid for twice already** —
  the self-snapshotting runner, the patch collection that tries every place
  the work could have landed, the `--network none` verifier, the qemu
  emulation guard, `report.py`'s rule that a missing grade is `rig`, and the
  gold-control-first discipline. They are imported with their comments.
- **The minimal rig carries the iteration disciplines** — the record contract
  (`record.jsonl`, `DONE`, `artifacts.sha256`), the egress proxy that this
  rig inverts, the dev/held-out split, one-hypothesis-per-iteration. Those are
  imported as files and rules, not by extending that repository in place (the
  brief is explicit: do not build inside the minimal-rig repo).

### The stale parts, verified before trusting (the brief's D1 requirement)

| claim from the brief | what I found on 2026-09-28 |
|---|---|
| `bench/deepswe/lib.sh` defaults `CORPUS` to `~/src/swe-pro/...`, which no longer exists | **Confirmed stale.** `lib.sh:8` still reads `$HOME/src/swe-pro/tools/deepswe-bench/tasks`; that path does not exist. The corpus is `~/Code/deep-swe/tasks` (117 directories). This rig reads its own `tasks/` and is unaffected. |
| `run.sh` passes `-db` and `-keep` to `codeaf do`, which the run engine refuses | **Confirmed stale.** `cmd/codeaf/do.go` refuses `--db` with exit 1 ("Drop --db, or set CODEAF_TASK_BELT=node to run this on the older engine", `do_engine_contract_test.go:360` pins it). Change entries `1416-do-door-keeps-its-contract.md` and `1355-the-worker-harness-is-the-default-belt-again.md` document it. **Resolution: this rig does not drive `codeaf do` at all** — the harness under test is senior-dev, so the `-db` question disappears; if a future arm wants the old engine, `CODEAF_TASK_BELT=node` is the documented word. |
| `~/Code/deep-swe` (Pier source) cited but unverified | **Present** — `LICENSE`, `PROVENANCE.md`, `README.md`, `tasks/` (the 117-task corpus, Harbor layout). The Pier *tool* is separately installed at `~/.local/bin/pier` and answers `--help`; its agents include `mini-swe-agent`, `codex`, `claude-code`; its environments include `docker` and `modal`; and its task config validates `allow_internet` — **Pier can run an arm with internet on**, which answers the brief's §3.3 question. |
| `~/Code/swe-pro-go-deepswe-minimal` exists with the iteration rig | **Present** — `iterate.sh`, `scoreboard.sh`, `diagnose.sh`, `delta-lib.sh`, `runpod/egress-proxy.go` (the proxy this rig inverts), `full113/`, `evalset.txt`. |

## 2. The task format

A superset of the Harbor/Pier layout the DeepSWE corpus uses, so a task
directory is both this rig's input and a Pier arm's input:

```
tasks/<task-id>/
  task.toml        the manifest (Harbor sections + our keys; see the file)
  instruction.md   the brief the agent reads — four sections: Task
                   description, Test guidelines, Lint guidelines, Style
                   guidelines, plus our own fair-internet-use text
  environment/     Dockerfile: the agent's container (base checkout, no
                   future history, build prerequisites installed, build
                   prewarmed)
  tests/           Dockerfile: the verifier image (built FROM the environment
                   image; the ONLY place the reference solution exists) plus
                   the scripts that generate the controls
  solution/        upstream-reference.patch provenance: the pinned commits,
                   never the bytes (they are fetched at image build time)
  rubric.toml      the rubric — see below
```

Environment hygiene (verified by an assertion in the image build): the tree is
checked out at the base commit with **no future history** — the clone's local
branch at the remote tip is deleted, the origin remote is removed, the fetch
bookkeeping files are gone, and `git gc --prune=now` prunes the commits past
the base; the build asserts `git cat-file -e <reference-commit>` fails and
that no refs remain. History BELOW the base stays: it is the repository's real
past. The build directory is prewarmed in the image so a grader's rebuild is
incremental (a measured 35 s full build; ~12 s incremental), and the image
asserts the prewarm left the tree byte-identical.

The repository sits at `~/repos/<name>` inside the container — the layout the
public task's own build command hints at (`make -C ~/repos/jsonschema`); it
costs nothing and keeps our briefs honest to the published shape.

## 3. The rubric

`rubric.toml` beside `task.toml`. Every criterion has an `id`, a `kind` (one
of the six FrontierCode kinds), `blocker = true|false`, a `weight`, and a
kind-specific spec:

| kind | spec | how the grader answers it |
|---|---|---|
| `command` | `command`, optional `timeout_sec` | runs in the task tree in the verifier container; exit 0 is pass |
| `classical` | `overlay` (the reference test-side patch), `run` | overlay applied idempotently (an agent that made the same test change already carries it — `git apply --reverse --check` decides), then the test command must exit 0. When the agent's own nearby test moved the overlay's anchor, the apply retries with one line of reduced context (`git apply -C1`); the test bytes are unchanged and the test still has to pass |
| `reverse-classical` | `run`, `revert_paths` | the named paths are restored to the base commit, the rebuild is forced (every reverted source touched — an incremental build would otherwise keep the patched binary and let the tests pass), then the tests must FAIL; failing is what proves they test the change |
| `adaptive-classical` | as classical | derived when the verbatim overlay applies and passes; otherwise the judge rewrites the test patch to the agent's interface (or refuses — a behavioural failure is not an interface divergence) and a second no-network container re-runs the adapted tests |
| `scope` | `max_files`, `max_changed_lines`, `allowed_paths`, `forbidden_paths` | computed off the patch itself, before any container |
| `prompt` | `question` (pinned), `paths` | the LLM judge, from the host: the criterion's question, the diff hunks for the named paths, and the mechanical excerpts phase A gathered (remaining `std::cerr` counts, helper-call counts) |

**Scoring.** FrontierCode publishes no weights; ours are recorded per rubric
(all non-blockers weigh 1 in this fixture). Blockers gate: any failed blocker
scores the run 0 regardless of the sum. Otherwise the score is the weighted
mean over non-blockers, rounded to 4 places.

**Every criterion's evidence is kept** — the check's own log under
`logs/grade/evidence/`, the judge's reasoning in the grade — so a 0 is
auditable without re-running anything.

**The judge.** Pinned per rubric: model `openrouter/anthropic/claude-sonnet-4.5`,
prompt version `fc-judge-1` (spelled in `grade/judge.py`), temperature 0. It
runs from the HOST — the verifier container has `--network none`, and the
judge is the one exception the brief allows. A judge call that fails leaves
the criterion `rig`, never a silent default. Its token usage is recorded in
every grade (measured: ~$0.006 per criterion call on the pinned judge).

## 4. Internet: open, logged, scanned

FrontierCode runs agents with internet on, and its own account says why a
blocklist is a dead end (their list passed a thousand domains and agents kept
finding ways around). So the policy is the one the brief prescribes:

**Open and logged.** `proxy/egress-proxy.go` (derived from the minimal rig's
allowlisting proxy, inverted): every CONNECT and plain request is PERMITTED
and WRITTEN DOWN — timestamp, target host, one line per connection, append-only.
The task container sits on an internal Docker network with no route out; the
proxy is the one exit. Non-proxy-honouring clients fail against the network —
the same containment cost DeepSWE accepted.

**Two levels of logging** (the brief's (a)/(b)):

- (a) the proxy log: hostname level. A MITM proxy with a CA in the container
  was considered and rejected for v1 — hostname-plus-transcript coverage
  catches the leak shape the minimal rig actually observed (a
  `raw.githubusercontent.com` fetch of the task's own tree), and a MITM turns
  the log from evidence into the thing under test.
- (b) the harness's own transcript: senior-dev's record (`records.jsonl`, the
  carried host's step stream — every model-written shell command) and its
  action log (`delegate-actions.jsonl`). Every `curl`, `git clone|fetch`,
  `npm|pip install`, and web-tool argument lands here in full.

**The scanner** (`grade/scanner.py`) runs after every agent run and flags:

- **hard** (scores the run 0, counts toward flag rate): the upstream slug
  (`owner/repo` from the task's `repository_url`) anywhere in the transcript;
  pull-request/commit/patch URL shapes that name the upstream; connections to
  the hosts that exist to serve repository content — `raw.githubusercontent.com`,
  `codeload.github.com`, `api.github.com`, `objects.githubusercontent.com`;
  and a run that produced NO proxy log at all (the logged-egress path did not
  run, which is a rig gap, said out loud rather than a quiet clean bill).
- **soft** (recorded, reported, not by themselves a zero): `github.com` and
  `gist.github.com` connections with no path evidence (hostname level cannot
  tell documentation from the fix), and package-registry installs
  (`registry.npmjs.org`, `pypi.org`, `files.pythonhosted.org`, `crates.io`,
  `proxy.golang.org`…) — Cognition's second named leak path is a registry
  install of a version containing the fix; v1 records installs for review
  rather than version-comparing them.
- **the model plane is not agent egress**: the guard (below), `openrouter.ai`
  and `models.dev` are excluded from flagging, still logged.

**The fair-use prompt** is our own wording, appended to every brief (see
`tasks/.../instruction.md`, "Internet use"): documentation, API references,
error lookups and background concepts allowed; the task's own upstream
repository, its mirrors, forks, issues, pull requests, commits, diffs,
changelogs and CI logs forbidden; a run that goes there scores zero whatever
else it did. Cognition's text was read and deliberately not copied.

**The credential guard.** The container holds NO provider key. The
conversation battery's guard (`bench/conversation/lib/guard.py`, run as a
container on the same internal network with the real key in its own
environment) holds the credential, refuses any model off the run's allowlist,
and meters every call with upstream's own usage rows
(`guard-usage.jsonl`). The harness gets a sentinel and a base URL that names
the guard. Its bind is widened to the container interface in the run's frozen
copy — the shared file is untouched. Cost is read TWICE for every run
(harness-reported from codeaf's own usage ledger, guard-metered from
upstream's usage) and a disagreement beyond $0.05 is written into the row.

## 5. The grader

Two phases, both `--network none`, in the verifier image (which is the only
image that contains the reference solution — the agent's environment image
never does):

- **Phase A** (`grade/rubric.py phase-a`): apply the patch (a patch that
  cannot apply to a pristine base is a legitimate 0, with the note saying what
  git saw; an EMPTY patch is a rig failure), then run every container-side
  criterion in rubric order — command, classical, reverse-classical, scope —
  and capture the judge's evidence while the tree is exactly the agent's (the
  first gold control graded its prompt criteria against the base tree because
  the evidence was gathered after the reverse check had reverted `src/`; the
  capture moved before any criterion runs).
- **The judge** (`grade/judge.py`, host): prompt criteria, one call each;
  and the adaptive path's test adaptation when the verbatim overlay did not
  fit.
- **Phase B** (`grade/rubric.py phase-b`): only when the judge adapted the
  tests — the adapted patch applied over the agent's tree, rebuild, rerun.
- **Combine** (`grade/rubric.py combine`): blocker gating, weighted sum, and
  the law that a criterion the machinery could not answer is `rig` — the run's
  score is then not computed at all, never averaged in as a 0.

**Adaptive classical in v1 is implemented and self-tested.** The first pilot
reached the adaptation branch for real — conflicted-files-refname-crash s1–s3
had the judge adapt the tests — and found the machinery broken at every seam:
phase B ran in a container with no `/logs/grade` and no adapted patch, did not
create its output directory, rebuilt with the fixture's recipe rather than the
task's, and `combine` misread `phaseB.json`. All of those are fixed and covered
by `grade/test_rubric.py`; `ATTEMPT-LEDGER.md` and the `REPORT.md` correction
carry the regraded outcomes. No live rollout has yet completed *through* phase B
to a verdict, because the same change's reduced-context overlay apply answers
the pilot's three conflicts before adaptation is needed. The honest statement
remains: phase B is machinery-tested and was exercised to its crash point live,
but no live run has finished with a phase-B verdict.

## 6. Controls

- **Positive** (`gold.sh`): the task's own reference solution must score
  **1.00** before any model tokens are spent. Run first, every time the rig or
  a task changes. Evidence: `results/jsonschema-log-warning-gold/`.
- **Negative** (`negative.sh`): the labelled example's shape — a generated
  patch that does every non-blocker thing right but converts each multi-line
  warning only down to its first line — must fail BOTH blockers and score
  **0.0** with every non-blocker passing. This is the calibration the brief
  demands: FrontierCode reports the equivalent patch met 8 of 10 criteria and
  failed both blockers; our grader must produce the same result. Evidence:
  `results/jsonschema-log-warning-negative/`.
- **Sealed fixture** (`seal.sh`): a hand-assembled run directory with a
  planted transcript (a step record whose command fetches the upstream PR
  diff; a web-tool call to `raw.githubusercontent.com/...`) and a planted
  proxy log (the model plane, the leak hosts, a registry host, and one
  innocent command) — the scanner must flag it, and the flagged run must
  score 0. Evidence: `results/jsonschema-log-warning-seal/`.
- **Scanner negative space**: the sealed fixture's transcript also carries an
  innocent command (`git log --oneline -3`) and its proxy log the model plane
  plus a registry host — the scanner's verdict separates hard from soft and
  leaves the model plane unflagged (visible in the seal's `scan.json`).

## 7. Protocol

- **Arms**: `codeaf-senior-dev` (the harness under test, driven through
  `codeaf senior-dev run --json --dir ~/repos/jsonschema --max-cost …
  --max-hours … --variant … --high <model> --asked "<brief>"`), and the
  baselines `mini-swe-agent` / `codex` through Pier on the same task
  directory, same model, same rubric, same grader.
- **Seeds and reasoning effort are the two swept dimensions** (the official
  protocol runs 5 trials per model per reasoning-effort level, averages the
  metric across the trials of a level, then reports each model's score at its
  best-performing level). The campaign manifest lists them as `seed_ids`
  (default 5) and `reasoning_efforts`; `launch.sh` enumerates every
  effort × seed × wave, one iteration label each, and `grade/report.py`
  prints the per-level mean score and mean output tokens and names the
  best-performing level. A one-element list pins a single trial — that is a
  canary, not the protocol. Results to date are single-trial (§12).
- **Columns**: score, pass (cleared every blocker), flag (scanner), cost per
  rollout — **harness-reported AND guard-metered, never an account-balance
  difference** — output tokens, wall seconds. `grade/report.py` prints the
  table; every column comes off the run's `record.jsonl` final row.
- **Missing grades are `rig`**, printed as `rig`, never averaged in as 0.
- **Comparison discipline**: tasks compared in pairs across arms; the
  preregistration rule and the dev/held-out split carry over from the minimal
  rig (D4 drafts the first preregistration).
- **Wall-clock comparability**: this rig's images run on the host's own
  architecture (arm64 here), so wall-clock numbers are comparable only to each
  other — the qemu emulation guard is carried over for the day an amd64 image
  is needed, but this rig's own fixture never takes that path.

## 8. What FrontierCode leaves unspecified, and what we chose

| unspecified | our choice, recorded here and in every grade |
|---|---|
| rubric weights | all non-blockers weigh 1; blockers gate and never enter the sum |
| the judge's model, prompt and threshold | model `openrouter/anthropic/claude-sonnet-4.5`, prompt `fc-judge-1` pinned in `grade/judge.py`, temperature 0, pass threshold = the criterion's own question |
| how "adapted" tests are made | the judge rewrites the reference test patch to the agent's interface, or refuses when the failure is behavioural; phase B re-runs the adapted tests in a fresh no-network container |
| time / turn / cost limits | per task: `agent.timeout_sec` (7200 s here), `--max-cost` ($5 default) and `--max-hours` (2) recorded per row; no turn limit |
| CPU and memory | 4 CPU / 8 GiB per container, in `task.toml`, recorded per row |
| the fair-use prompt's text | our own wording, in each task's `instruction.md`; deliberately not Cognition's |
| the scanner's exact rules | hard/soft split in `grade/scanner.py` §4 above; flag ⇒ score 0 |
| whether the agent's build reformats sources (this project's compile target runs `clang-format -i`) | the format criterion runs BEFORE the build, on the patch as graded; the patch collection takes the agent's tree AFTER whatever its own build did, which is the change a maintainer would receive |
| what "prompt criteria" review | the diff hunks of the named paths plus mechanical excerpts; nothing else |

## 9. Cost model (measured on this rig, 2026-09-28)

- **Grading controls cost nothing but CPU**: gold and negative run two
  incremental builds + tests in-container (~40 s each on this machine) and one
  judge call per prompt criterion (~$0.006 each on the pinned judge).
- **A live rollout**: harness-metered and guard-metered cost are both
  recorded per row; on the DeepSWE history model (`deepseek/deepseek-v4-flash-0731`)
  expect roughly $0.2–1.5 per rollout on a C++ task of this size (the
  DeepSWE full113 campaign's median was lower — this task builds a C++
  project). The judge adds ~$0.02–0.05 per grade.
- **A pilot** (8 tasks × 2 arms, PLAN-pilot.md): ~$5–15 of model spend at
  2 seeds, within the $20 cap the brief set. Running the official protocol's
  5 trials per model per effort multiplies the rollouts (80 at one effort) and
  waits for the owner's budget approval. Any campaign beyond that waits for
  the owner's approval.

## 10. The D2 acceptance checks and where their evidence lives

All four passed on 2026-09-28. A result directory is evidence, never a scratch
pad: every row below lives in its own directory, and a rerun goes to a new
seed tag rather than overwriting one (`pier-arm.sh` refuses an existing
directory outright).

| check | expected | evidence |
|---|---|---|
| (a) gold clears all blockers | score **1.00** | `results/jsonschema-log-warning-gold/` — `logs/grade/grade.json` (score 1.0, no failed blockers, no rig criteria), `logs/grade/phaseA.json` (9 pass / 0 fail / 0 rig), `logs/grade/judge.json` |
| (b) first-line-only patch | score **0.0**, both blockers failed, non-blockers pass | `results/jsonschema-log-warning-negative/` — `grade.json` (score 0.0, `failed_blockers` = both), judge reasoning naming the leftover `std::cerr` lines |
| (c) sealed fixture | **flagged**, scores 0 | `results/jsonschema-log-warning-seal/` — `scan.json` (hard reasons: slug in transcript, patch-shape URL, `raw.githubusercontent.com` connection), `record.jsonl` final row (score 0.0, flagged) |
| (d) one live rollout | a complete result row | `results/jsonschema-log-warning-codeaf-senior-dev-deepseek-deepseek-v4-flash-0731-s3/` — `logs/grade/grade.json` (1.0, all twelve criteria pass), `scan.json` (unflagged), `cost.json` (both readings), `record.jsonl`, `DONE`, `artifacts.sha256` |

### The scoreboard after the first day

| row | arm | model | score | cost | wall | note |
|---|---|---|---|---|---|---|
| gold | reference patch | — | 1.00 | $0 | 41 s | the grader's calibration |
| negative | first-line-only patch | — | 0.00 | ~$0.02 | 40 s | both prompt blockers failed |
| seal | planted-leak fixture | — | 0.00 | $0 | — | flagged by the scanner |
| s3 | codeaf senior-dev | deepseek/deepseek-v4-flash-0731 | **1.00** | $0.044 | 1141 s | self-check passed, egress clean |
| s1 | mini-swe-agent via Pier | deepseek/deepseek-v4-flash-0731 | **1.00** | $0.052 | 1295 s | Pier's egress posture, see PIER-BASELINE.md |

Two earlier senior-dev rolls (s1 at $0.071, s2 at $0.116, both scored 1.00 by
this rubric) predate two fixture fixes below; their rows were superseded by s3
rather than deleted — the append-only records still carry them.

## 11. What the first day surfaced (fixture findings, fixed in the same change)

- **`xxd` was missing from the environment image.** Three base-tree tests
  (`pass_schema_less_jsonl*`) shell out to `xxd`, so the FULL suite failed with
  `xxd: not found` on a pristine checkout while the graded reference tests
  passed. Two senior-dev rolls terminated `fail` — "submitted a change that the
  project's own build or tests do not pass" — over a change the rubric scored
  1.00 twice; the disagreement traced to this gap, not to the change. The image
  installs `xxd` now, the full 301-test suite exits 0 on the base tree, and the
  next roll (s3) finished with the harness's own verification passing.
  The lesson is the brief's own: the environment must run the project's own
  full suite clean, because the harness under test may pin exactly that.
- **A result directory was shared between two arms** (the name lacked the arm),
  and the second arm's grade overwrote the first's patch and grade files. Both
  scripts now put the arm in the directory name and `pier-arm.sh` refuses an
  existing directory. The mixed first-day artifacts were split; s3 reran clean.

## 12. What is stubbed or not yet done

- **Adaptive classical** is implemented and machinery-tested; the first pilot
  took the adaptation branch live and crashed in phase B, and this change fixed
  every seam it hit. No live rollout has yet finished *through* phase B to a
  verdict (§5).
- **Registry-install version comparison**: the scanner records registry
  installs but does not yet compare installed versions against the base
  commit's dependency pins.
- **The corpus**: one fixture task exists. Task sourcing is a drafted pipeline
  in PLAN-pilot.md, not a corpus.
- **MITM-level (path-level) egress logging**: hostname + transcript only, by
  design (§4).
- **One trial per arm so far**: each arm ran once on this fixture, at effort
  `high`. The rig now sweeps `seed_ids` × `reasoning_efforts` and averages per
  level (§7), but no campaign has run the official 5-trials-per-level protocol
  yet; PLAN-pilot.md sets the seed budget per task.
## 13. Conformance with FrontierCode

The official specification is Cognition's announcement,
<https://cognition.com/blog/frontiercode>. The requirements below were read
there on 2026-06-08; a re-fetch from this environment on 2026-09-29 returned
404, so this table rests on that capture and should be re-checked against the
live page when it answers again. One row per official requirement; a row is **conformant** (we do the same thing),
**adapted** (the same intent, different mechanism, stated) or
**not-closable** (we cannot honestly produce it, and will not pretend to).

| official requirement | our status |
|---|---|
| Endpoint: mergeability — "would the maintainer actually merge this PR", judged on correctness, test quality, scope discipline, style and adherence to codebase standards | **conformant** — the six-kind rubric (§3) grades exactly those five facets on the agent's patch |
| Grading ensemble: unit tests, rubrics, and "new types of verifiers" | **adapted** — unit tests are the classical/reverse-classical/adaptive-classical verifiers and the rubric is the prompt + scope criteria; we have no mechanism beyond those two families |
| Two metrics: pass (cleared all blocker criteria) and score (weighted aggregate of rubric items, 0 when a blocker fails) | **conformant** in rule — `grade/rubric.py combine` gates on blockers and computes the weighted mean (§3); both metrics are in every `grade.json` row and in `grade/report.py` |
| Rubric weights | **adapted** — FrontierCode publishes no weights; ours are recorded per criterion and are uniformly 1 in the fixture, so our score is a plain mean. The scoring rule (weighted aggregate, blockers gate to 0) matches the official semantics; the specific weights are ours |
| Protocol: 5 runs per model at every available reasoning effort, metric averaged per effort, best-performing level reported | **conformant in mechanism, not yet exercised** — `seed_ids` (default 5) and `reasoning_efforts` are swept dimensions of a campaign (§7); every campaign so far ran one trial at one effort. No 5-trial result exists yet |
| Output tokens reported as the mean per rollout | **conformant** — `completion_tokens` per run off the final record row; `grade/report.py` prints it per run and as a per-level mean |
| Internet: runs flagged for unfair internet use receive zero | **adapted** — our egress is open and logged (§4) and the scanner flags upstream-diff evidence, patch shape and registry leaks; a flagged run scores 0 and a seal control proves it (§6) |
| Task provenance: 20+ open-source maintainers authored the tasks from repos they maintain, 40+ hours per task, each repo's own definition of mergeable | **not-closable** — our tasks are mined from merged PRs of repos we do not maintain; nobody with maintainer authority defined what "mergeable" means for them |
| Task count and subsets: Extended 150, Main 100, Diamond 50 | **not-closable** — one built fixture (`tasks/jsonschema-log-warning`) and seven unbuilt candidates (PLAN-pilot.md); no subset structure, no rate is estimable |
| Quality control: adversarial testing, calibration, multi-stage review, a manual researcher review per task | **not-closable** — we have three automated controls (gold, negative, seal, §6) and no human review pipeline; they calibrate the grader, they do not validate the task |
| Task rubric authorship: the repo's maintainer writes the rubric | **not-closable** — our rubrics are written by us, the harness developers, from reading the upstream change |

The judge — model `openrouter/anthropic/claude-sonnet-4.5`, prompt `fc-judge-1`
— is our own choice, pinned and recorded per grade (§3, §8): the official
specification does not name a judge model.

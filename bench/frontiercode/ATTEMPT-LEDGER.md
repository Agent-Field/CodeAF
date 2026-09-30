# Per-attempt ledger — FrontierCode pilot, 2026-09-29

This is the audit the corrected report is built on. Every row is read from the
retained evidence under `bench/frontiercode/evidence/`, not from a summary:
`meta.json`, `record.jsonl` (the final row), `logs/grade/grade.json` and
`scan.json`. Where a claim cannot be re-derived from retained bytes, this file
says so.

## What the campaign ran

Three tasks × five seeds = **15 trial identities**, one arm
(`codeaf-senior-dev`), one model (`deepseek/deepseek-v4.1-flash`), one reasoning
effort (`high`). The retained identities are the second execute wave; see
"Discarded first attempts" below.

## The 15 identities, categorised

Legend: **orig** = the original `logs/grade/grade.json` verdict; **flag** =
`scan.json.flagged`; **final** = the committed `record.jsonl` final row, where a
flag zeroes the score (`grade/finalize.py`); **corrected** = this change's
regrade of the retained `model.patch` (conflicted task only — the other two
tasks' criteria never touched the fixed paths).

| # | task | seed | orig status | orig score | flag | flag reason kind | final score | final pass | corrected score | corrected status |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | conflicted-files-refname-crash | s1 | rig | — | yes | hard: slug + github-family egress | 0.0 | no | 0.75 | done |
| 2 | conflicted-files-refname-crash | s2 | rig | — | yes | hard: patch-shape inside an observation | 0.0 | no | 0.75 | done |
| 3 | conflicted-files-refname-crash | s3 | rig | — | yes | hard: patch-shape inside an observation | 0.0 | no | 1.0 | done |
| 4 | conflicted-files-refname-crash | s4 | done | 1.0 | no | — | 1.0 | yes | 1.0 | done |
| 5 | conflicted-files-refname-crash | s5 | done | 0.75 | yes | hard: patch-shape inside an observation | 0.0 | no | 0.75 | done |
| 6 | jsonschema-log-warning | s1 | done | 1.0 | no | — | 1.0 | yes | — | — |
| 7 | jsonschema-log-warning | s2 | done | 1.0 | no | — | 1.0 | yes | — | — |
| 8 | jsonschema-log-warning | s3 | done | 1.0 | no | — | 1.0 | yes | — | — |
| 9 | jsonschema-log-warning | s4 | done | 1.0 | no | — | 1.0 | yes | — | — |
| 10 | jsonschema-log-warning | s5 | done | 1.0 | no | — | 1.0 | yes | — | — |
| 11 | shell-pipe-empty-command | s1 | done | 1.0 | yes | hard: api.github.com connection | 0.0 | no | — | — |
| 12 | shell-pipe-empty-command | s2 | done | 1.0 | yes | hard: api.github.com connection | 0.0 | no | — | — |
| 13 | shell-pipe-empty-command | s3 | done | 1.0 | yes | hard: api.github.com connection | 0.0 | no | — | — |
| 14 | shell-pipe-empty-command | s4 | done | 1.0 | yes | hard: api.github.com connection | 0.0 | no | — | — |
| 15 | shell-pipe-empty-command | s5 | done | 0.0 | yes | hard: api.github.com connection | 0.0 | no | — | — |

Category counts (a run can carry two tags):

- **Grading failure (rig)** — 3: identities 1–3. All are
  `conflicted-files-refname-crash` s1–s3, and all three failed at the adaptive
  overlay path (see the corrected regrade below).
- **Model failure** — 1: identity 15. `shell-pipe-empty-command` s5 failed its
  blocker `empty-command-skipped-not-crashed` honestly (the agent skipped the
  empty command instead of crash-vs-erroring on it); score 0.0.
- **Scanner flag** — 9: identities 1, 2, 3, 5, 11–15. All nine are zeroed to
  0.0 in the final record by policy.
- **Completed grade** — 12 by the original verdict (all but 1–3); **15** after
  the corrected regrade, which answers the three rig identities.
- **Discarded first attempt** — 0 among these 15; see below.

## Denominators (explicit, consistent)

| quantity | original evidence | corrected regrade |
|---|---|---|
| trial identities | 15 | 15 |
| grading failures (rig, no score) | 3 | 0 |
| numeric grades | 12 | 15 |
| scanner-flagged | 9 | 9 (unchanged; policy) |
| unflagged | 6 | 6 |
| pass by rubric (score > 0, no failed blocker, pre-flag) | 11 | 14 |
| pass by final record (flag zeroes) | 6 | 6 |
| mean over numeric rubric scores | 10.75 / 12 = **0.8958** | 13.25 / 15 = **0.8833** |
| mean over the 15 final-record scores | 6.00 / 15 = **0.4000** | 6.00 / 15 = **0.4000** |

The two pass/mean rows answer two different questions and must not be mixed:

- **Rubric view** (what the grader said about the work): 11/15 original, 14/15
  corrected; mean 0.8958 original over the 12 numeric, 0.8833 corrected over 15.
- **Policy view** (what the record ships): 6/15 both ways; mean 0.4000, because
  a hard scanner flag is a zero whatever the rubric said.

Infrastructure failure is never a model zero: the three rig identities carry no
score and are excluded from the rubric mean, not averaged in as 0.

## Reconciliation with `REPORT.md`

The "Full pilot run" section's aggregate line is **"11 passing (…); mean score
0.83 over the 13 trials with a numeric score"**. Against the retained bytes:

1. **"11 passing"** is the rubric view over all 15 (identities 4–14). It is not
   the final record's pass count, which is 6 (identities 4, 6–10) because the
   flag zeroes the other five rubric-passers (5, 11–14). Both are true of
   different questions; the report does not say which.
2. **"13 trials with a numeric score"** is wrong: the retained evidence has
   **12** numeric `grade.json` scores (identities 4–15). Three are rig (1–3),
   not two. The stated mean 0.83 is the right numerator over the wrong
   denominator (10.75 / 13); the correct original mean is **10.75 / 12 =
   0.8958**.
3. The report's per-task table prints each run's **`grade.json` score** beside a
   flag column, so flagged rows read "1.00 … flagged yes" — a state the final
   record never holds. The final row zeroes every flagged run. The table should
   carry both the rubric score and the shipped (post-flag) score, as this ledger
   does.
4. The report's failure list calls the three conflicted s1–s3 rig failures
   "adaptive overlay conflict … left untouched per the no-scoring-change rule".
   That was the correct description of the defect; this change fixes the
   machinery, and the corrected regrade below supersedes it.

## Discarded first attempts

The first execute wave of the campaign produced an **empty `model.patch` for
every run** — `collect.sh` still hardcoded the single-task-era checkout path.
That wave was discarded and the whole wave re-run from scratch (`REPORT.md`,
"What broke" #1; commits `609c0d279`, `f8c0f64f7`, `3d19ba65f`). Those attempts
were a rig failure, not model results, and their directories were overwritten by
the re-run, so they are **not** among the 15 retained identities. They are
recorded here as a campaign-level rig event; the 15 identities are all the
second wave.

## Scanner flags: policy outcomes, and which are demonstrably false positives

The scanner (`grade/scanner.py`) is the benchmark's leak safeguard. Its flags
are **policy outcomes**, not grading verdicts, and are kept as such — this change
does not weaken or retune them. What the retained bytes show:

- **Identities 11–15 (`shell-pipe-empty-command`)**: hard flag
  `proxy: connection to api.github.com` plus the slug corroboration. `api.github.com`
  is on the scanner's hard host list (it exists to serve repository content), so
  the flag follows the written policy. The raw `egress-proxy.log` was **not
  committed** for these runs (the repository-wide `*.log` ignore dropped it; see
  the evidence-completeness fix in this change), so what made the connection
  cannot be re-derived from the branch — only `scan.json` remains. Unresolved,
  recorded as a provenance limitation, not as a model failure.
- **Identity 1 (conflicted s1)**: hard flag "upstream slug present alongside
  github-family egress". The proxy counts (18 hosts) corroborate github-family
  egress; the raw log is again missing, so the connection's cause is unresolved.
- **Identities 2, 3, 5 (conflicted s2/s3/s5)**: hard flag "patch/commit shape in
  `delegate-actions.jsonl`/`records.jsonl`". The matched text is **the task's own
  source file** returned to the agent: `pre_commit/git.py` embeds
  `github.com/pre-commit/pre-commit/issues/300` in its docstring/comment, and the
  agent's `read`/`sed` observation carries that text. The scanner scans whole
  JSONL records, so a URL found inside a tool *observation* is treated as the
  agent *mentioning* it. No github-family connection appears in these runs'
  proxy logs (`proxy_hosts` 2–4, all registry/model-plane), so the agent never
  reached the upstream. These three flags are **demonstrably false positives of
  the observation-vs-action distinction**. Per the rig's own stated policy
  ("do not change rig scripts to make a failure go away"), the scanner is left
  as it is and the finding is recorded here and in the report.

## Related hazards reviewed

- **`rubric.py phase_a` UnboundLocalError for an unappliable patch.** Confirmed:
  `judge_input` was built only on the success branch, so a patch that did not
  apply raised before `phaseA.json` was written and grade.sh recorded `rig`
  instead of the legitimate 0 the code intends. Fixed (the key is initialised
  before the apply) and regression-tested.
- **`allowed_paths ['./']` prefix semantics.** Confirmed: `allowed_paths = [""]`
  matched every path by accident (`startswith("")`), while `allowed_paths =
  ["./"]` matched none (patch paths never carry `./`), and `forbidden_paths =
  [""]` would have forbidden everything. `scope_prefixes` now drops `''`, `'.'`
  and `'./'` (meaning "no restriction") and strips a leading `./` from real
  prefixes. None of the three pilot tasks used `./`; the fix is for the draft
  candidates that do.
- **Multi-commit patch ranges.** Reviewed and not applicable to the pilot's
  three tasks: each was collected as `git diff --binary <base> <ref>`, a single
  base..head diff, and the retained `model.patch` files apply cleanly to their
  bases. The candidate corpus already records this as fixed for the multi-commit
  PRs (kong, cobra, bubbletea, clap) in
  `docs/changes/unreleased/1700-frontiercode-five-candidates.md`.

## Corrected regrade of the retained conflicted patches

`regrade-retained.sh` regrades a retained `model.patch` in the task's verifier
image with the fixed grader, reusing the run's original `judge.json` (no judge
call, no provider spend) and writing to `evidence-corrected/<run>/` — the
original `evidence/<run>/` is untouched. Commands and results:

```
bash bench/frontiercode/regrade-retained.sh \
  evidence/conflicted-files-refname-crash-codeaf-senior-dev-deepseek-deepseek-v4.1-flash-s1-ehigh
```

| seed | original | corrected | correcting criterion note |
|---|---|---|---|
| s1 | rig | 0.75 | `overlay applied with one line of reduced context (-C1); tests exit 0`; scope 22 > 20 |
| s2 | rig | 0.75 | same apply; scope 25 > 20 and an out-of-scope path |
| s3 | rig | 1.0 | same apply; scope 19+/1- in 2 files |
| s4 | 1.0 | 1.0 | unchanged (no conflict) |
| s5 | 0.75 | 0.75 | unchanged (no conflict) |

The fix removes the grading failure (rig) but does **not** change the shipped
score for s1–s3: all three carry hard scanner flags, so the final record still
zeroes them. The corrected rubric scores are 0.75 / 0.75 / 1.0; the corrected
shipped scores are 0.0 / 0.0 / 0.0. The point of the regrade is that the work is
now measurable, not that the flag went away.

The phase-B branch (the one the three conflicts would have taken before the
reduced-context apply answered them) is reproduced end-to-end by
`tests/phase-b-smoke.sh`: a forced overlay conflict in phase A, the retained
`adapted-tests.patch`, and the fixed grader — phase B runs in a fresh
no-network container, applies the adapted patch, the tests pass, and `combine`
answers the conflicted classical criterion from the phase-B verdict (score
0.75). It makes no judge call and no provider spend.
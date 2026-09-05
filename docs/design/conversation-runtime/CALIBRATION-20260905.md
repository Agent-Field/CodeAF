# Calibration 02 — measured candidate and next decisions

Candidate `ec9f5a0c2`, local branch `santosh/conversation-runtime`. The full repository check passed before the binary and rig were frozen. This is calibration, not a demonstrated Pareto frontier.

## Protocol and completeness

36 planned cells completed: six scenarios × three harnesses × two randomized repetitions. Every inference role was pinned to `deepseek/deepseek-v4-flash-0731`, low requested effort, through an exact-model guard. Native Claude Code Opus was used separately for implementation/review. Each cell used fresh owned state and the same fixture and permissions; runs were sequential. Runtime cap was 180 seconds per cell; interactive fixtures included a 60-second command.

Executable/package and rig hashes, machine identity, conditions and seeded order are retained in the frozen manifest. Provider routing was each harness’s default stack, not a common pinned backend. Fresh local state does not guarantee a cold provider cache. Two repetitions of one fixture are not two independent kinds of task.

All 36 attempts are retained. Billing is complete for 35 cells. One timed-out Aforge revision cell has 32 of 35 calls priced; a later read-only reconciliation did not recover the missing prices. Its total remains unknown. No failure was retried away or relabeled. No confidence or equivalence claim is made from n=2.

## Results

Each row is one declared slice. Time is mean end-to-end door duration, including launch/quiet confirmation/teardown; it is not the delay to a correct chat reply. Cost is mean billed cost across both attempts, withheld when either is incomplete. Success requires the full objective check set and a successful door exit.

| Scenario | Harness | Passed | Mean seconds | Mean cost, USD |
|---|---|---:|---:|---:|
| data-tally | aforge | 2/2 | 6.9 | 0.0008495 |
| data-tally | pi | 2/2 | 5.1 | 0.0002165 |
| data-tally | omp | 2/2 | 5.7 | 0.0004795 |
| research-brief | aforge | 2/2 | 11.2 | 0.0020309 |
| research-brief | pi | 2/2 | 5.3 | 0.0002885 |
| research-brief | omp | 2/2 | 8.0 | 0.0025340 |
| writing-memo | aforge | 2/2 | 12.2 | 0.0017029 |
| writing-memo | pi | 2/2 | 10.3 | 0.0005000 |
| writing-memo | omp | 2/2 | 11.0 | 0.0012760 |
| code-fix | aforge | 2/2 | 12.1 | 0.0020092 |
| code-fix | pi | 2/2 | 8.5 | 0.0003926 |
| code-fix | omp | 2/2 | 12.7 | 0.0013067 |
| followup-while-working | aforge | 1/2 | 98.9 | 0.0031573 |
| followup-while-working | pi | 0/2 | 81.3 | 0.0001557 |
| followup-while-working | omp | 0/2 | 82.5 | 0.0016784 |
| revision-midwork | aforge | 1/2 | 160.1 | unknown |
| revision-midwork | pi | 2/2 | 83.1 | 0.0004841 |
| revision-midwork | omp | 2/2 | 82.7 | 0.0014754 |

All print cases passed. On these observed blocks Pi was faster and cheaper than Aforge on each print slice; Aforge’s mean cost was about 3.4–7.0× Pi’s depending on the slice. This identifies development work; it does not establish population-level dominance.

## Chat failures and timing

- Both Pi side-question answers were correct but arrived after the command finished (about 62 seconds after submission). Both OMP runs missed the exact answer. Aforge passed one of two: the passing answer was first observed after 5.293 seconds while work continued. The other answer was wrong. These outcomes cannot be pooled into an acceptable conversational reliability claim.
- In Aforge the new grace handoff consumed steering after 1.550 seconds in the first run and 1.133 seconds in the second. A prior diagnostic waited 30.621 seconds. These are trace observations with a deterministic regression behind the fix, not a controlled latency effect estimate. The ~3-second rule bounds handoff of already-running bash; other tools, provider latency and display polling still affect visible response.
- Both Pi and OMP revision cases passed, taking about 83 seconds. Aforge took 181.612 seconds and timed out once, then passed in 138.608 seconds. In the timed-out case the final CSV was correct and Markdown absent, but the task/completion flow did not settle inside the cap. It remains a timeout, not a pass based only on the files.
- The timeout trace opened task 2 after the main turn had already written the CSV and removed Markdown. Its brief chiefly waited for the existing job and checked those artifacts. The task ran for about 146 seconds and went through further review of whether the script’s own stdout proved completion. This is evidence of costly unnecessary handoff and repeated checking, not evidence that more workers are needed.

## What to change next, in order

1. **Stop converting small finishing work into new tasks.** Inspect the remaining-work handoff boundary, not just its counters. The current write allowance sees both an old and revised filename in the same turn, and can move a turn whose useful edits are already complete. Awaiting an existing job or reporting its result belongs to its existing owner. Add real-turn regressions for a revised small deliverable and for a genuinely long editing job that must leave chat available. Do not merely raise the threshold until this fixture passes.
2. **Reduce the rendered prompt/tool contract before adding discovery machinery.** One research trace sent roughly 15k input tokens and 31 tools per main call. Compare concise equivalent definitions against the current contract on held-out small and task workloads, keeping authority, permissions, required evidence and capability unchanged. Record actual request bytes, billed tokens, cache hits, calls and correct outcomes. A shorter string is not automatically a quality-preserving improvement.
3. **Measure the corrected cache accounting and routing policy.** The post-calibration change below bounds credit by the actually observed input length and keeps each request’s lineage with its own settlement. Its performance effect is unmeasured. Test history-rewrite invalidation and absent versus reported-zero cache usage separately. A healthy-lane release after a reported miss and soft provider fallback are policies to test, not proven bugs from one trace.
4. **Keep optional post-answer work off the response path without losing continuity.** Title and memory work caused about three seconds of one print run’s exit tail. This is lower priority for chat because its answer had already been emitted. Measure overlap under existing bounded cleanup; do not delete memory just to make a print benchmark faster.
5. **Calibrate meaningful parallel and nested work.** The reviewed `multi-defect-pipeline` fixture is available by explicit selection; it is a correctness fixture, not proof of useful parallel speedup, and has not been compared live. Substantial held-out coding and noncoding work still needs a serial Aforge ablation. Test actual child-room steering, parent integration of a new revision, publication races, long tool output/compaction, cancellation and restart.
6. **Confirm only after quality gates improve.** Freeze candidate, workloads and practical margins; run repeated paired blocks across time windows and held-out variants. Include all failed-attempt costs. Broader writing/research quality and human intervention burden require blinded human rubrics. Do not buy a large confirmation run while the small chat gates are failing.

## Validation and evidence

Full `make check`: **PASS**, `candidate-make-check-03.log`, with the unchanged known-red ledger. Session tests 180.440s; terminal tests 465.761s; verification tests 3.170s; packed manual 1.486s. Binary 50,518,578 bytes under the unchanged 54,600,000-byte budget. The tested checkout stayed unchanged during the run. The preserved unrelated `internal/.DS_Store` modification accounts for the binary’s dirty marker.

Python measurement regressions: 64 passing tests before the campaign; offline runner: 102 checks passed. Steering race tests reproduced the young-command delay, replaced-timer callback, and stop/detach race before their fixes; the final focused lifecycle set passed with the race detector.

Evidence root: `/private/tmp/af-conversation-ops/pareto-campaign/`. Frozen manifest: `calibration-02.json`. Raw results and paired descriptive report: `calibration-02/results.jsonl` and `calibration-02/summary.txt`. Root-reviewed routing analysis: `opus/ROUTING-CACHE.md`. Late recovery ledger: `calibration-02/018-revision-midwork-aforge/evidence/revision-midwork-aforge/guard-reconciled-late.jsonl`.

All work remains local. Shared `dev` and the original consolidated QA checkout were not modified.


## Changes and validation after the measured run

Source `3cb79f3f8` adds two corrections after the frozen calibration:

- Cache estimates cannot credit more input than a provider actually reported receiving for the remembered conversation. Newly appended context is no longer credited merely because the conversation identity matches. Missing lengths earn no credit. This remains an estimate: compaction, rewriting and eviction can invalidate a prefix.
- Provider settlement carries a typed observation containing its own conversation identity and reported prompt/cache counts. The shared last-request-per-model map was removed. Previously overlapping requests could credit A’s result to B’s conversation. A barrier-based stub test forces out-of-order completion, fails with the old attribution, and passes in both streamed and whole-body paths; five repetitions with the race detector passed.

The optional `multi-defect-pipeline` scenario contains four small independent defects and a combined CLI. Its 29 external cases compare input/output/status in a separate parent process. The original judge was rejected after unrepaired code rewrote its in-memory tests into no-ops; the committed regression preserves that counterexample. Further regressions cover import hangs, descendants that ignore termination, exited parents, and judge termination. Process-group cleanup is best effort on POSIX; a deliberately detached descendant is outside that guarantee. The fixture directory is not a filesystem sandbox, and it makes no claim about substantial parallel work or internal API coverage beyond the CLI.

Final combined validation on `3cb79f3f8`:

- `make check`: **PASS**, `final-make-check.log`. Session 180.339s; provider 55.734s; lane 7.236s; terminal 465.866s; verification 3.167s; packed manual 1.481s. Binary 50,518,274 bytes, below the unchanged 54,600,000-byte budget.
- Python benchmark suite: **92 passed**, `final-python-tests.log` (48.188s).
- Offline benchmark runner: **112 passed, zero failed or skipped**, `final-rig-selftest.log`.
- The full provider package under `-race` is **not green**: `TestTheWarmTranscriptEncodeCostsTheSameAtEightyTurnsAsAtEight/breakpoints` fails allocation-count equality. The root reproduced that same failure on unchanged `389ab7594` in `cache-race-baseline.log`; normal full checks and the targeted new race tests pass. No known-red entry or test skip was added.

The six revision artifacts were also parsed independently after the campaign: every header and row was exact, every superseded Markdown file was absent, and each prepared command ran once. `calibration-02/revision-artifact-adjudication.json` retains those checks. The timeout verdict remains unchanged because its completion flow exceeded the cap.

All delegate changes were reviewed before local merge; the owned retired worktrees and their binaries were removed. The integration binary remains `/private/tmp/af-conversation/bin/aforge`. These follow-on changes have **no live performance result yet**. The measured table above remains attached to `ec9f5a0c2`; documentation-only commits after final validation do not change that distinction.

# What the first two sweeps show

Ten runs: five DeepSWE tasks × two seeds, `aforge do` headless with every model
knob pinned to `deepseek/deepseek-v4-flash`, in each task's own pinned container,
graded by that task's verifier image. s1 on `44e6a4f4`, s2 on `5fa2561e`.
The grading path is proved by `gold.sh`: all five reference solutions score 1.

**Nothing scored 1. Nothing was close to running out of budget.** The five tasks
carry a 90-minute agent wall each; the median run settled at **10 minutes** having
spent **8 cents**. That is the finding the rest of this file is about.

## The table

| task | seed | reward | f2p | p2p | cost | wall | exit | nodes | test runs | broke at |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| ofetch-per-origin-circuit-breaker | s1 | 0 | 41/47 | 13/13 | $0.054 | 220s | 0 | 3 | 11 | **settlement** — `task-38-x1` |
| ofetch-per-origin-circuit-breaker | s2 | 0 | 42/47 | 12/13 | $0.054 | 591s | 0 | 1 | 7 | **settlement** — `task-2` |
| ink-grid-box-layout | s1 | 0 | 17/25 | 49/49 | $0.168 | 1528s | 0 | 1 | 31 | **settlement** — `task-2` |
| ink-grid-box-layout | s2 | 0 | 5/25 | 49/49 | $0.080 | 622s | 0 | 1 | 4 | **settlement** — `task-2` |
| textual-richlog-follow-state | s1 | 0 | 2/20 | 2/6 | $0.062 | 837s | 0 | 1 | 9 | **leaf tool use** — `task-2` |
| textual-richlog-follow-state | s2 | 0 | 2/20 | 2/6 | $0.087 | 580s | 0 | 1 | 9 | **leaf tool use** — `task-2` |
| igel-persist-feature-schema | s1 | 0 | 5/24 | 2/2 | $0.080 | 246s | 0 | 2 | 11 | **settlement** — `task-39` |
| igel-persist-feature-schema | s2 | 0 | 0/24 | 2/2 | $0.195 | 1659s | 0 | 1 | 33 | **leaf tool use** — `task-2` |
| happy-dom-deterministic-intersectionobserver | s1 | **VOID** | — | — | $0.625 | 5400s | 124 | 6 | 71 | **split-continuation** — `task-2-x1-n2` ran on `swe` |
| happy-dom-deterministic-intersectionobserver | s2 | *still running at write time* | — | — | — | — | — | 1 | 90 | — |

`nodes` excludes the permanent root. `test runs` counts bash tool calls whose
command actually invokes the project's suite. Node counts, worker kinds and spend
are read from each run's own store; f2p/p2p come from the verifier's `reward.json`.

Worker check for the void rule: every node in every s1/s2 run ran on `bare` or
`linear` **except** happy-dom s1's `task-2-x1-n2`, which the escalation ladder
handed to `swe` (`run.log`: `↻ Core engine — escalated linear → swe: escalated
from linear after a failed attempt`). That one run is void; the other eight
graded runs are valid measurements of the path under test.

## The one finding

**Every run ends by declaring victory over work it has not done, and the exit code
agrees with it.** Eight of eight graded runs exit 0 — "delivered whole" — with
reward 0. Seven of eight settled with the review gate having already said so:

> `gate: refused — what the review asked for next is not in the request`

The review is right every time. igel s1, `task-39`, at 4m6s:

> The deliverable does not contain the actual code changes that implement the
> feature schema persistence and validation rules.

The run then wrote its deliverable — "All 41 tests pass. Here's a summary of what
was done" — and exited 0. The gate is advisory: it identifies the gap, the
harness declines the correction on the ground that "what the review asked for next
is not in the request", and settles. That single sentence appears in ofetch s1,
igel s1, ink s1, ink s2, textual s1, textual s2, and happy-dom s1.

Three consequences fall out of it, and they are the whole scoreboard:

1. **The budget is never used.** 220s–1659s against a 5400s wall. Nothing was cut
   short; every run chose to stop. Raising the wall buys nothing.
2. **The deliverable is a summary, not the work.** ink s1's final message opens
   "All 23 tests pass. The grid layout support is already fully implemented" while
   8 of 25 hidden tests fail. ofetch s1's opens "All 51 tests pass".
3. **A near-miss and a no-show are indistinguishable from outside.** ofetch (41/47)
   and textual (2/20) both report `settled: true`, exit 0.

## (a) happy-dom: is the planner re-splitting?

No. There is no growth loop. Over 90 minutes s1 recorded **two** `subtree_spliced`
events, and s2 recorded **one**:

```
02:19:24  subtree_spliced  task-2       -> [task-2]
02:44:49  subtree_spliced  task-2-x1    -> [n1, n2, n3, n4, task-2-x1]
```

The 3-and-9 pending counts are the four children of one splice plus their
synthesis node waiting behind a leaf that will not finish. What actually consumed
the wall is a **leaf that restarts and starts over**:

```
node_started task-2          02:19:24   then again 02:39:34
node_started task-2-x1-n2    03:02:55   then again 03:23:10, then 03:43:20
node_completed task-2-x1-n2  03:49:18   (2s before the wall)
```

Three starts of the same node, twenty minutes apart to the minute, and the stream
shows the reason each time: `last call deepseek/deepseek-v4-flash 15m24s ago`. A
provider call hangs, a twenty-minute watchdog restarts the leaf, and the leaf
begins again rather than resuming — 71 test runs of the same file with nothing to
show. s2 shows the same shape at half the scale: one node, two starts, 90 test
runs, no splice at all.

So the growth the settle lane governed is not what is happening here. The two
mechanisms actually visible are **a hung provider call with no per-call deadline**
and **a restart that discards the leaf's work**. The escalation after the second
failed attempt is what put `task-2-x1-n2` on `swe`.

**The s3 binary does refuse the escalation.** With `work.workers: bare` pinned in
the profile, `ofetch` s3 and `textual` s3 both print
`↻ … — handed to bare: escalated from linear after a failed attempt`: the ladder
still escalates, but it can only reach an installed worker.

## (b) ofetch 41→42 of 47: which tests, and did the leaf run them?

The leaf ran the right file — 11 test invocations in s1 including
`npx vitest run test/circuit-breaker.test.ts` and four full-suite `npx vitest run`
— but it ran **its own** `test/circuit-breaker.test.ts`, which it wrote, and which
the verifier replaces with the hidden one. The model's suite went green on the
model's own reading of the brief.

The six that fail in s1 (five in s2) are one family: **the failure counter is not
incremented where the brief says it should be.**

| failing test | verifier's reason |
| --- | --- |
| counts onRequestError hook exceptions as failures | expected to throw /Circuit breaker is open/ but got 'onRequestError hook failure' |
| does not retry when onRequestError hook throws | same |
| does not close half-open on rejected non-listed statuses | expected fn called 3 times, got 2 |
| tracks circuit state independently per origin | expected fn called 2 times, got 1 |
| uses final failed retry time when a half-open probe re-opens | expected fn called 3 times, got 2 |

s2 fixed the hook-exception pair and broke a pass-to-pass test in exchange
(`only treats configured failureStatusCodes as status-based failures` now rejects
with "Circuit breaker is open" where it must resolve). Same 0.900 partial, moved
around. This is the closest any run came, and it came in **220 seconds**.

## (c) igel 5 → 0: what s2 changed

s2 worked three times longer (1659s vs 246s), spent 2.4× more, made 33 test runs
against s1's 11 — and scored worse, because it refactored further. Every one of
the 24 fail-to-pass tests now fails **on setup**:

```
AttributeError: <class 'igel.igel.Igel'> has no attribute 'results_path'
```

The hidden tests reach for an existing class attribute the s2 patch removed. s1
left it alone and five tests passed. Nothing in the run notices: the leaf's own
tests do not touch `results_path`, so the regression is invisible from inside and
the run settles with `p2p 2/2` green — the pass-to-pass whitelist is only two
tests wide, which is exactly the case where a leaf's own suite is the only
regression signal there is.

textual is the same failure in a different repo: `AttributeError: 'RichLog' object
has no attribute '_size_known'` on four pass-to-pass tests, both seeds. The patch
removed a private attribute the widget already had.

## (d) Did the leaf run the project's own tests before settling?

Yes, in all ten runs, and the count is not the problem.

| run | bash calls | test runs | edits | reward |
| --- | --- | --- | --- | --- |
| ofetch s1 | 47 | 11 | 13 | 0 |
| ofetch s2 | 17 | 7 | 17 | 0 |
| ink s1 | 133 | 31 | 36 | 0 |
| ink s2 | 38 | 4 | 15 | 0 |
| textual s1 | 34 | 9 | 7 | 0 |
| textual s2 | 52 | 9 | 21 | 0 |
| igel s1 | 43 | 11 | 11 | 0 |
| igel s2 | 86 | 33 | 33 | 0 |
| happy-dom s1 | 189 | 71 | 23 | void |
| happy-dom s2 | 204 | 90 | 10 | — |

The correlation with reward is nil, and where it exists it is backwards: igel s2
tested three times as often as igel s1 and scored 5 → 0. What the runs test is
their own new file, narrowly (`npx ava test/grid.tsx`, `pytest test_feature_schema.py`,
`vitest run test/intersection-observer/IntersectionObserver.test.ts` seventy-one
times). What none of them does is run the repository's full suite before declaring
done and treat a regression as a blocker — which is precisely the signal that
would have caught igel s2 and both textual runs.

## Rig defects hit, and what they were

| defect | evidence | fix |
| --- | --- | --- |
| Host had no amd64 emulation; every task image is amd64-only | `exec /bin/uname: exec format error` | register the binfmt handler once: `docker run --privileged --rm tonistiigi/binfmt --install amd64` |
| Under qemu a threaded Go program dies in the collector — esbuild sits under vitest, so a whole TS suite reported "no tests" and **the reference solution graded 0** | `runtime: lfstack.push invalid packing`, ofetch gold reward 0 | `GOGC=off` plus a `GOMEMLIMIT` backstop in both containers; measured 3/3 reward 1 after, 0/2 with `GOMAXPROCS=1` alone |
| A brief that opens with a "-" bullet is parsed as a flag | ink s1 died in 1s: `flag provided but not defined: - Update the display style property…` | feed the brief on stdin, which `do` accepts when no positional argument is given |
| Five simultaneous pulls of five 3 GB images | `toomanyrequests: Rate exceeded` | skip the pull when the image is local, retry with backoff otherwise |
| `report.py` scored a missing `reward.json` as `?` instead of a rig failure | `default=None` fell through to `{}` | sentinel object, compared by identity |
| Gold rows were being averaged into the solved count | five guaranteed ones in every table | report.py skips `*-gold` unless named |
| **Editing `run.sh` while runs were in flight corrupted them** | `./run.sh: line 145: is: command not found`, then a phase re-logged an hour later; happy-dom s1 never extracted its patch, graded, or wrote meta | bash reads a script by byte offset. The rig must snapshot itself at launch and re-exec from the copy. Not yet done — s3 is running from these files and must not be touched. happy-dom s1 was recovered by hand from its still-live container. |

## Where this leaves the harness

Three things to fix, in the order the evidence ranks them:

1. **Settlement.** A run whose review names missing work must not settle, and a run
   that settles at 4% of its budget with the gate refused must not exit 0. This is
   seven of the eight graded failures.
2. **Regression signal.** Two runs shipped a patch that deleted an attribute the
   repository already had. Running the repository's own suite once, before
   settling, and refusing to settle on a new failure would have caught both.
3. **Liveness.** A provider call that hangs for fifteen minutes should not cost
   twenty, and the restart that follows should not throw the leaf's work away.

---

# s3 — the worker roster pinned (`27e43a9c`)

Same five tasks, same model, `"work.workers": "bare"` written into the profile so
the specialist worker is not installed at all.

| task | seed | reward | f2p | p2p | cost | wall | exit | nodes | test runs | broke at |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| ofetch-per-origin-circuit-breaker | s3 | 0 | 37/47 | 12/13 | $0.234 | 1633s | 0 | 3 | 33 | **settlement** — `task-2-x2` |
| ink-grid-box-layout | s3 | **RIG** | — | — | $0.458 | 4280s | 0 | 3 | 67 | **rig** — patch did not apply |
| textual-richlog-follow-state | s3 | 0 | **17/20** | **6/6** | $0.147 | 1503s | 0 | 2 | 18 | **settlement** — `task-2-x1` |
| igel-persist-feature-schema | s3 | **RIG** | — | — | $0.129 | 1910s | 0 | 4 | 0 | **rig** — patch did not apply |
| happy-dom-…-intersectionobserver | s3 | 0 | **12/14** | 9/9 | $0.049 | 912s | 0 | 1 | 12 | **settlement** — `task-2` |

## The roster held

**No `swe` node in any s3 store** — not run, not even planned. `nodes.subharness`
and `nodes.splice_subharness` across the five stores contain only `bare` and
`linear`, where every s1/s2 store carried `splice_subharness = swe` on its leaves
and happy-dom s1 actually ran one there. The ladder still escalates; it can only
reach an installed worker:

```
s1  ↻ Core engine  — escalated linear → swe: escalated from linear after a failed attempt
s3  ↻ Add follow state — handed to bare: escalated from linear after a failed attempt
```

## And it changed the scores

Two of the three tasks that graded moved a long way, and both moves trace to the
escalation landing on `bare` instead of stalling or leaving:

- **textual 2/20 → 17/20 f2p, and 2/6 → 6/6 p2p.** Both earlier seeds shipped a
  patch that deleted `RichLog._size_known` and never noticed. s3 escalated to
  `bare` after a failed attempt, got a second node (`task-2-x1`), ran 18 test
  invocations against 9, and the regression is gone. Three fail-to-pass tests
  short of reward 1 — the best result of the sweep.
- **happy-dom graded at all, at 12/14 and 9/9 in 912 seconds.** s1 spent the whole
  90-minute wall on a leaf that restarted three times and ended void; s2 was lost
  to a rig defect. With one node and no escalation, s3 came two tests short.
- **ofetch went the other way**, 41/47 → 37/47, at 4× the spend and 7× the wall.

## What did not change

The settlement finding is untouched. All three graded runs exit **0** with reward
0, and ink s3 is the clearest statement of the problem in the whole corpus — the
gate caught the same lie three times and the run settled on it three times:

```
1h2m6s   gate: fail — The deliverable reports that everything is committed, but the
                      run record shows the commit command returned an error and no
                      successful commit is recorded.
1h7m25s  gate: pass
1h10m9s  gate: fail — The deliverable claims the commit exists (commit `32df26b`),
                      but the run record shows no successful commit was made.
1h11m19s ✓ Commit
```

Two earlier `gate: refused — what the review asked for next is not in the request`
at 30m11s and 50m22s. The run then exited 0.

## Two more rig defects, both found by s3

| defect | evidence | fix |
| --- | --- | --- |
| The graded diff was taken without `--binary`, so any run that wrote a non-text file produced `Binary files a/x and b/x differ` and `git apply` refused the **whole** patch | igel s3 (`model_results/feature_schema.joblib`) and ink s3 both graded `apply_failed=1` — two of five runs lost | `git diff --cached --binary`, which is what the corpus's own collect command in `task.toml` uses |
| qemu dumps a core file into the working directory when an emulated process crashes, and `git add -A` sweeps it into the graded diff | ink s3's patch carried `qemu_node_20260829-045200_38233.core` — a node crash under emulation, not the agent's work | exclude emulator core dumps before staging |

Neither is a model failure. igel s3 and ink s3 are recorded as rig failures, not
zeroes, and both need re-running on the fixed rig before anything is concluded
from them.

---

# s4 — the settlement and liveness fixes (`7c498557`)

Same five tasks, same model, roster still pinned to `bare`, rig now snapshotting
itself and taking the diff with `--binary`.

| task | seed | reward | f2p | p2p | cost | wall | exit | nodes | test runs | broke at |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| ofetch-per-origin-circuit-breaker | s4 | 0 | 41/47 | 12/13 | $0.053 | 603s | 0 | 1 | 14 | **settlement** — gate passed a wrong answer |
| ink-grid-box-layout | s4 | 0 | 7/25 | 49/49 | $0.128 | 1033s | 0 | 1 | 18 | **gate broke** — delivered unjudged |
| textual-richlog-follow-state | s4 | **none** | — | — | $0.000 | 230s | **1** | 0 | 0 | **planner broke** — no work ever started |
| igel-persist-feature-schema | s4 | 0 | 6/24 | 2/2 | $0.122 | 1263s | **2** | 4 | 0 | **honest partial** — repair exhausted |
| happy-dom-…-intersectionobserver | s4 | 0 | **13/14** | 9/9 | $0.099 | 1759s | 0 | 1 | 30 | **settlement** — gate passed a wrong answer |

## The settlement fix works, and it is visible

igel s4 is the first run in twenty to end the way a failed run should. The whole
chain is in the stream:

```
18m57s  no more rounds — this work has split as many times as splitting helps
                       — handing over what's done
21m2s   gate: refused — The deliverable is a plan for what to run next, not the
                       finished work itself. … it describes what will be done
                       rather than carrying the completed changes
                       — no more work could be started on it
exit 2
```

And the deliverable now says so in its own voice, where every earlier run said
"All 41 tests pass":

> I'm handing this over with a reservation — a review found this still missing:
> … I've taken it as far as repair takes it: no more work could be started on it.

That is the finding named, the repair rounds accounted for, and the exit code
telling the truth. **One run of five.**

## What the other four say

- **ofetch and happy-dom still exit 0 on a wrong answer**, because the gate
  *passed* them. happy-dom's gate failed the first attempt with an accurate
  finding at 27m58s ("The deliverable is a list of file paths, not the
  implementation itself"), the run repaired, and the gate passed at 29m17s a
  deliverable claiming "All tests pass (31/31)" — 13 of 14 hidden tests pass.
  ofetch's gate passed a deliverable claiming "All 56 tests pass" at 41/47. The
  refusal path is fixed; the **acceptance** path is now the hole.
- **happy-dom s4 is one test short of a solve** — the best result of any sweep,
  at $0.10 and 29 minutes.

## The new dominant failure: the model answers, and it is not JSON

Three different places in the harness now break on the same thing, and two of
them cost the whole run:

| where | line | cost |
| --- | --- | --- |
| plan fan-out | `splice failed: plan request: fan-out stage 2: response contains no JSON object (finish_reason=length completion_tokens=16384)` | **textual s4: exit 1, zero nodes, $0.0003, nothing attempted** |
| delivery gate | `note: the delivery gate did not judge task-2 — the gate answered with nothing this could read: response contains no JSON object; delivering unjudged` | **ink s4: the gate is skipped and the run delivers anyway, exit 0** |

The planner case is the worse of the two: the fan-out response was cut at the
completion cap (`finish_reason=length`, 16384 tokens), and rather than retry with
a smaller ask the run gave up before a single node existed. textual had scored
17/20 on the previous sweep.

The gate case quietly converts a graded run into an ungraded one — the whole
point of the settlement fix is the gate, and a gate that answers with
unparseable text is treated as an abstention rather than a fault.

## Liveness: not exercised

No `✗ … retried` fault line appears in any s4 run (`fault_retries: 0` in all five
`meta.json`). No provider call hung this sweep, so the cut-and-retry path had
nothing to fire on — untested, not broken. happy-dom s4's `task-2` did restart
once, and no resume line was printed, so whether it came back to banked work
cannot be read off the stream.

The before/after regression finding did not appear either: no run's stream
carries a `regression` or turned-red line. The two regressions this corpus knows
about (`RichLog._size_known`, `Igel.results_path`) were both in runs that did not
reach that stage this sweep.

## Sweep over sweep

| task | s1 | s2 | s3 | s4 |
| --- | --- | --- | --- | --- |
| ofetch | 41/47 | 42/47 | 37/47 | 41/47 |
| ink | 17/25 | 5/25 | rig | 7/25 |
| textual | 2/20 | 2/20 | **17/20** | planner died |
| igel | 5/24 | 0/24 | rig | 6/24 **(exit 2)** |
| happy-dom | void | lost | 12/14 | **13/14** |

Nothing has scored 1 yet. Two tasks are now within three tests of it, and the
run that is furthest from it is the one that never started.

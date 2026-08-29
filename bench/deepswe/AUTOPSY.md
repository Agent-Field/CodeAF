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

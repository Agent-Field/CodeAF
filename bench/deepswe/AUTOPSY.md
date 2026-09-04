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

---

# s5 — acceptance and shaped answers (`b910ccf8`)

Same five tasks, same model, roster `bare`, rig snapshotting. happy-dom was still
running when this was written and is appended below.

| task | reward | f2p | p2p | cost | wall | exit | nodes | acceptance points | `no check exercises` | `↻` shaped |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| igel-persist-feature-schema | 0 | **23/24** | 2/2 | $0.107 | 1470s | **2** | 3 | 4 | 0 | 0 |
| ofetch-per-origin-circuit-breaker | 0 | **44/47** | 13/13 | $0.132 | 1190s | 0 | 2 | 52 | 0 | 0 |
| ink-grid-box-layout | 0 | 7/25 | 49/49 | $0.135 | 1207s | 0 | 1 | 7 | 0 | 0 |
| textual-richlog-follow-state | 0 | **1/20** | 2/6 | $0.152 | 1189s | 0 | 4 | 5 | 0 | 1 (escalation, not a shaped repair) |

Two runs are now within three hidden tests of a solve — igel at 23/24 is the
closest anything has come. Nothing has scored 1.

## (a) Every gate line, its evidence, and whether the evidence was true

**igel** (`run.log` 40, 49, 71)

| line | node | verdict | evidence cited | true? |
| --- | --- | --- | --- | --- |
| 40 | `task-2` | fail | "the record shows no branch was created or committed — `git status` was run multiple times and `git diff master --stat` was attempted, but no `git checkout -b` or `git commit` appears" | **true** at that point |
| 49 | `task-2-x1` | fail | "the named files (feature_schema.joblib, description.json) were not produced" | **false** — `igel/feature_schema.py`, `igel/igel.py`, `igel/servers/fastapi_server.py` and `tests/test_igel/test_feature_schema.py` are all in the graded diff; the gate was reading the *current node's* record, and the files were written by `task-2` |
| 71 | `task-2-x2` | refused | "the run record shows no pytest output — the last command ran pytest but its output is not shown (only the command and a timeout flag)" | **true** |

**ofetch** (43, 53)

| line | node | verdict | evidence cited | true? |
| --- | --- | --- | --- | --- |
| 43 | `task-2` | fail | "The project's own verification (`pnpm test`) exited 1 and named 0 checks — tests did not pass. The deliverable claims 'all tests pass' but the record shows the test suite failed." | **true, and this is the mechanism working exactly as designed** — the claim bought nothing, the gate read the run |
| 53 | `task-2-x1` | fail | "it does not contain the test output, the verdict on whether tests pass, or any evidence that the work was exercised" | **true** |

**ink** (47) — one gate, `task-2`, fail: "The deliverable is a listing of files, not the answer itself … The fenced text contains only a file listing and a line 'Now let me rebuild and test:'". **True.**

**textual** (45, 55, 71, 75) — four gates on four nodes. The first three are true
("the fenced text is a summary of what was done, not the code itself"). The
fourth is the important one:

> `gate: refused — The deliverable reports that examples/rich_log_follow_state.py exists and is committed, but the run record shows nothing of that name was produced — it is absent from what was left behind.`

**False.** The graded diff contains `diff --git a/examples/rich_log_follow_state.py`,
and the run's own artifact list names it. The reading it used was the **last
node's** record: `task-2-x3` ("Deliver code") produced nothing, because every
`write`/`edit` in the whole run belongs to `task-2`. The gate asked "what did
this node leave behind" and reported the answer as "what exists". The same
per-node reading produced igel's false finding at line 49.

## (b) The refusal grounds, and the code that produced them

Two `gate: refused` lines this sweep, and they take **different paths with
opposite consequences**:

| run | refusal ground | code | `Overturned` | exit |
| --- | --- | --- | --- | --- |
| igel | `the same words were already worked on once` | provenance refusal — declines to buy a round, checks nothing in the world | false | **2** |
| textual | `everything it names is already in the delivered text, in the words the request used` | `internal/revision/judge.go:967`, `AdmitGapPresent` | **true** | **0** |

`AdmitGapPresent` reads the gate's citations, extracts enumeration items
(`enumerationItem` regex, `enumerationFloor = 3`), lowercases both sides, and if
**every** item is a substring of the deliverable it returns that sentence and
sets `Overturned`. `store/gate.go` documents `Overturned` as "CHECKED AGAINST THE
WORLD … the file the review says is missing is on disk under the name the request
used". For textual it was checked against **the deliverable's prose, not the
filesystem** — and the deliverable is a summary that names the file it claims to
have written. So a *false* finding about a file was overturned by confirming the
words for that file appear in the text that claims it. The one check that would
have settled it — is `examples/rich_log_follow_state.py` on disk? — is the check
that was not run, and it would have said the file is there and the finding is
wrong for a different reason.

## (c) Why exit 0 after a fail

`cmd/aforge/do.go:1552` — `deliveredWhole` returns false only when the last gate
is `!Pass && !PolishClosed && !Overturned`. The four last-gate records:

| run | pass | polish_closed | refused | overturned | unclosed | mechanical | ⇒ exit |
| --- | --- | --- | --- | --- | --- | --- | --- |
| igel | false | false | "the same words were already worked on once" | — | — | — | **2** |
| ofetch | false | **true** | — | — | — | — | 0 |
| ink | false | **true** | — | — | — | — | 0 |
| textual | false | false | "everything it names…" | **true** | — | — | 0 |

igel is the fix working: a provenance refusal leaves the finding standing and the
run exits 2. The other three exit 0 through two different doors:

- **`PolishClosed` (ofetch, ink)** — "A gap the one polish pass closed delivers
  whole because the work was redone." But *redone* is not *closed*: ofetch's
  polish round moved 41→44 of 47 and ink's left 7 of 25, and neither gap was
  re-judged. The flag records that a repair ran, not that it worked.
- **`Overturned` (textual)** — the word-containment acquittal above, on a run
  that scored 1/20.

No `Unclosed` and no `Mechanical` was set on any run this sweep.

## (d) The verification readings

The photograph is real but **leaves no record in the store**: there is no
`reading`, `roster` or `verification` event kind in any s5 graph.db, and
`verify.Reading` is carried in the gate's in-memory `Evidence` only. The only
place it surfaces is gate prose, and it surfaced exactly once:

> ofetch, line 43: "The project's own verification (`pnpm test`) exited 1 and
> **named 0 checks**"

Command `pnpm test`, exit 1, roster **empty**. No other run's stream mentions a
reading, a before/after pair, or a check turning red.

**textual lost 16 fail-to-pass tests between s3 (17/20) and s5 (1/20), and
nothing in the run saw it.** The regression mechanism subtracts `Before.Failing`
from `After.Failing` and needs `Taken && AfterTaken`; with no reading recorded
and no roster reported, `Regressed()` returns nothing by construction. The
photograph that would have caught it was never developed.

## (e) Why zero `no check exercises` findings

Structural, and it is one line of control flow. `settleAcceptance`
(`internal/revision/acceptance.go:311`) is reached **at one moment: after the
model judge has said the deliverable is whole.**

> "A gate that is already failing the work buys the repair round anyway, so
> asking the coverage question there would spend a call to reach a conclusion
> that is already true."

**Not one gate passed in s5** — all ten `delivery_gate` events across the four
runs carry `pass: false`. So `settleAcceptance` was never entered, `MapChecks`
was never called, no `exercises` field appears on any gate event, and
`no check exercises` was unreachable for the whole sweep.

It would have been unreachable a second time even if a gate had passed. The
mapping needs `CheckEvidence`, which is `verify.PatchChecks(patch)` plus the
reading's roster — and ofetch's roster was **0 checks**. With `len(checks) == 0`
the function returns early with

> `pass.Unmeasured = "nothing in this project's verification could be read, so no check could be matched to what the request asked for"`

which is a note on a pass, not a finding.

The checklist itself is built and journaled on every run, and its point counts
are uneven against the graded surface in a way worth recording:

| run | acceptance points | hidden f2p | note |
| --- | --- | --- | --- |
| ofetch | 52 | 47 | tracks the request clause for clause |
| happy-dom | 16 | 14 | close |
| ink | 7 | 25 | one point per bullet; the bullets are coarse |
| textual | 5 | 20 | stops after the second paragraph's first two clauses |
| igel | 4 | 24 | covers only the first paragraph |

Three of five derive a checklist far coarser than what is graded, so even a
working mapping could not have raised a finding about most of what these runs
missed.

## Shaped answers: not exercised

No `↻ … answer cut at the ceiling — continued` line, no re-ask, and no
`response contains no JSON object` in any s5 run. The single `↻` is textual's
worker escalation (`↻ follow-state — handed to bare: escalated from linear after
a failed attempt`, line 26). Every structured call this sweep came back readable,
so the seam had nothing to do — untested, not broken. s4's two failures of this
kind (planner fan-out, delivery gate) did not recur.

## happy-dom s5 (appended)

| task | reward | f2p | p2p | cost | wall | exit | nodes | acceptance points | `no check exercises` | `↻` shaped |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| happy-dom-…-intersectionobserver | 0 | **13/14** | 9/9 | $0.074 | 1961s | **2** | 3 | 16 | 0 | 0 |

Three gates on three nodes (`run.log` 49, 79, 86), and it ends the way igel does.

- **L49 `task-2`, fail** — "The deliverable states 'All 38 tests pass' but the
  project's own verification (`npm run test`) exited 1 and **named 0 checks** —
  the tests did not pass." True, and the second independent sighting of the
  reading: two runs, two runners, both rosters empty.
- **L79 `task-2-x1`, fail** — "The deliverable is a plan for what to do next, not
  the finished work itself … only a message about checking whether vitest works."
  True.
- **L86 `task-2-x2`, refused** — `the same words were already worked on once`,
  `pass=false, polish_closed=false`, no `Overturned` → `deliveredWhole` false →
  **exit 2**. The finding it left standing ("the deliverable does not report what
  came back — whether tests passed or failed") is true.

So the exit-code fix fires on two of five runs, and both times through the
provenance door rather than a repaired gap. Acceptance derived 16 points against
14 hidden tests — the closest match in the set — and still raised nothing,
because no gate passed and the roster was empty. One test short of a solve.

### s5 in one line

Two runs exit 2 honestly (igel 23/24, happy-dom 13/14). Three exit 0: two on
`PolishClosed` (a repair ran, the gap was never re-judged) and one on
`Overturned` by word-containment over a false file finding. Every run's roster
was empty or unread, so no coverage finding was reachable and no regression was
visible.

---

# s6 — one reading, four blind spots (`b8af707b`)

| task | reward | f2p | p2p | cost | wall | exit | nodes | acceptance points | reading events | `unmeasured` gates | last line |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| ofetch-per-origin-circuit-breaker | 0 | **44/47** | 13/13 | $0.313 | 2808s | **2** | 13 | 47 | 0 | 0 | `partial —` |
| ink-grid-box-layout | 0 | **22/25** | 49/49 | $0.850 | 5403s | **2** | 8 | 9 (×5 rounds) | 0 | 4 | `partial —` |
| textual-richlog-follow-state | 0 | **18/20** | 6/6 | $0.085 | 1204s | 0 | 2 | — | 0 | 0 | — (exit 0) |
| igel-persist-feature-schema | 0 | 6/24 | 2/2 | $0.226 | 2222s | **2** | 4 | 17 | **8** | 0 | `partial —` |
| happy-dom-…-intersectionobserver | 0 | **13/14** | 9/9 | $0.413 | 5407s | **2** | 12 | 21 (×3 rounds) | 0 | 3 | — (`settled: false`, wall) |

Four of five now exit 2. Every task is within a few tests of a solve except igel,
which fell from 23/24 to 6/24. Nothing has scored 1.

## What landed

**(a) The exit code and the gate line share one reading.** Three runs end with
the new line, suffix intact:

> `partial — gate: … The missing element is the file itself. (not repaired: what the review asked for next is not in the request)` — igel, `run.log:94`

**(c) A repair that moved nothing cannot close a world-grounded finding.**
`unmoved: true` appears on igel's last gate and on two of happy-dom's, and
`unclosed: true` on happy-dom's and ink's — the two fields the exit code turns
on, now being set rather than inferred.

**(g) The checklist derives per behaviour.** igel 4 → 17 points, ofetch 52 → 47
— exactly the number of hidden fail-to-pass tests. ink 7 → 9 and happy-dom
16 → 21 against 25 and 14.

**(e) Acceptance settles on a failing verdict, and Unmeasured reaches the record.**
igel's first gate carries `exercises: 17 rows, 3 unmapped`, fourteen of them
mapped to real `tests/test_igel/test_feature_schema.py::TestFeatureSchema::…`
identities. ink and happy-dom carry the other half of the mechanism on seven
gates between them:

> `unmeasured: "nothing in this project's verification could be read, so no check could be matched to what the request asked for"`

**(b) A file finding is no longer overturned by prose.** textual's last gate is
`refused — what it asked for is already on disk under the name the request used`,
`overturned: true` — and it is TRUE: the files are in the graded diff. The s5
failure (a false "nothing of that name was produced" acquitted because the words
appeared in the summary) did not recur in that form.

## What did not land

**(d) The runner-native reading fired on one project in five.** Only igel
journaled anything — eight events, and they are exactly the record that was
missing:

| when | runner | command | declared | exit | named | red | inherited |
| --- | --- | --- | --- | --- | --- | --- | --- |
| before the job's first change | pytest | `python3 -m pytest -rA` | `make test` | 1 | 2 | 2 | — |
| on the finished tree | pytest | `python3 -m pytest -rA` | `make test` | 1 | 33 | 2 | — |
| before (round 2) | pytest | same | `make test` | 1 | 2 | 2 | **true** |
| on the finished tree (round 3) | pytest | same | `make test` | 1 | 36 | 5 | — |

The baseline is taken once and correctly `inherited` into each repair round —
mechanism (f) working. It also shows the reader going behind the project's own
spelling: declared `make test`, ran `python3 -m pytest -rA`.

**textual (pytest), ofetch (vitest), ink (ava) and happy-dom (turbo monorepo)
journaled nothing at all** — not a reading, and not a row saying why one could
not be taken. ink and happy-dom at least surfaced `unmeasured` on their gates;
ofetch and textual recorded neither, so from their stores alone a project that
declares no verification is still indistinguishable from a reader that failed.

**No `no check exercises` finding was raised in any run**, including igel's,
whose mapping had three unmapped points sitting in the gate record. And **no
regression finding fired anywhere** — igel's own readings show red going 2 → 5
on the finished tree, but those three are tests the run itself added, so the
subtraction is correctly empty. The mechanism is untested rather than wrong.

**One false file finding survived.** igel `run.log:41`: "The request asked for
feature_schema.joblib to be written in the results directory after fit. The
record shows nothing of that name was left behind — the file was not produced."
`model_results/feature_schema.joblib` is in the graded diff.

## The cost of the rounds

ink spent **$0.850 and its whole 90-minute wall** across five acceptance rounds
and five restarts of `task-2`, for 22/25 — 38.7M prompt tokens. happy-dom spent
$0.413 and also hit the wall, ending `settled: false`. The repair machinery now
buys rounds that the run cannot finish inside its budget, which is a new failure
shape: s5's runs stopped too early, s6's two largest stop only because time ran
out.

## Forensics: where s6's largest two runs spent their money

ink s6 cost $0.850 and 38.76M prompt tokens; happy-dom s6 cost $0.413. Both hit
the wall. The question is whether that is restarts starting over, or repair
rounds each re-reading the repository. **It is the first, and the second is not
what it looked like.**

### ink s6 — `task-2` restarted four times, each from turn 1

Not a watchdog and not a hung call: the run has **zero `✗` lines** and no reaper,
stale or timeout marker anywhere. Each restart is a release immediately followed
by a re-claim under a new token, in the same second:

```
07:08:03 node_started  task-2 token=1
07:30:13 node_released task-2 token=1 next=2   →  node_claimed token=3  →  node_started
07:52:23 node_released task-2 token=3 next=4   →  node_claimed token=5  →  node_started
08:14:34 node_released task-2 token=5 next=6   →  node_claimed token=7  →  node_started
08:36:40 node_released task-2 token=7 next=8   →  node_claimed token=9  →  node_started
```

The gaps are **22m10s, 22m10s, 22m11s, 22m06s** — a fixed ceiling, not an event.
It is not a turn or token ceiling: attempt 1 reached turn 72 and attempt 2
reached turn 138 in the same 22m10s, and `leaf_mode {"mode":"open","turns":200,
"tokens":150000}` is re-emitted unchanged on every claim. The first restart
happened at 07:30:13, **before any `job_growth`** (round 1 was granted at
07:42:04), so the restarts are not caused by the repair rounds.

**Nothing is banked.** Turn numbers reset to 1 four times, and attempt 2's
turn 1 is, verbatim:

> "I'll start by exploring the codebase to understand the current structure and
> how styles are handled" → `cd /app && git branch -a && git log --oneline -5`

| attempt | started | turns reached | prompt tokens | cached | cost |
| --- | --- | --- | --- | --- | --- |
| 1 | 07:08:03 | 1–72 | 5,557 | 0 | $0.000 |
| 2 | 07:30:13 | 1–138 | 11,106,468 | 10,634,752 | $0.231 |
| 3 | 07:52:23 | 1–98 | 11,063,439 | 10,423,808 | $0.247 |
| 4 | 08:14:34 | 1–97 | 11,898,991 | 11,081,984 | $0.270 |
| 5 | 08:36:40 | 0–21 | 4,683,243 | 4,442,880 | $0.101 |

Three full attempts at ~11M prompt tokens each, every one of them beginning by
re-reading the repository it had already read.

### happy-dom s6 — same shape, and the rounds are cheap

`task-2` restarted twice on the same cadence (22m11s, 21m59s) with five turn-1
resets in its transcript, and its second attempt alone spent **10,663,109** of
the run's prompt tokens. It then decomposed into eight leaves, and those leaves
are the cheap part:

| node | prompt tokens | cost |
| --- | --- | --- |
| `task-2` attempt 2 | 10,663,109 | $0.224 |
| `task-2` attempt 3 | 2,735,372 | $0.082 |
| `task-2-x3` | 242,793 | $0.008 |
| `task-2-x2` | 221,476 | $0.007 |
| `task-2-x1-n6` | 1,409,262 | $0.044 |
| `task-2-x1-n5` | 645,957 | $0.019 |
| `task-2-x1-n2` | 608,403 | $0.017 |
| `task-2-x1-n7` | 414,967 | $0.013 |
| `n1`, `n3`, `n4` | 0 | $0.000 |

None of the eight leaves restarted. Together they cost $0.108 — an eighth of what
one monolithic leaf's single attempt cost.

### The verdict

**Restarts from scratch, not acceptance rounds.** The repair machinery is not
re-reading the repo; the decomposed leaves it produces are small and cheap. What
is expensive is one undecomposed leaf that is cut at a ~22-minute ceiling, thrown
away, and started again from "let me explore the codebase" — three times in ink,
twice in happy-dom, at ~11M prompt tokens a go.

Two things follow. The ceiling is a **liveness defect wearing a budget's
clothes**: nothing was hung, so nothing needed restarting, and the cut is on a
timer rather than on evidence. And the restart is a **memory defect**: the leaf
is re-claimed with the workspace it already changed but with none of the
transcript that says what it changed or why, so it pays the exploration cost
again and can contradict its own earlier decisions — which is the most plausible
reading of igel falling from 23/24 to 6/24 between sweeps.

Caching hides most of the price (≈95% of these prompt tokens were cached, which
is why $38.76M of ink's tokens cost 85 cents rather than $3), so the cost signal
understates how much work is being repeated. The wall does not: both runs spent
their entire 90 minutes and neither finished.

---

# s7 — the reading arrives on two runners, and the ceiling becomes the wall (`9fcd4675`)

| task | reward | f2p | p2p | cost | wall | exit | nodes | acceptance pts | readings (read/total) | last line |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| igel-persist-feature-schema | 0 | **23/24** | 2/2 | $0.148 | 2210s | 0 | 5 | 17 | **8 / 8** | — (gate passed) |
| ofetch-per-origin-circuit-breaker | 0 | 41/47 | 13/13 | $0.113 | 1206s | **2** | 4 | 54 | **4 / 4** | `partial —` |
| ink-grid-box-layout | 0 | 13/25 | 49/49 | $0.195 | 1248s | 0 | 2 | 9 | 0 / 2 | — (gate passed) |
| textual-richlog-follow-state | 0 | 2/20 | 2/6 | $0.109 | 956s | **2** | 4 | — | 0 / 0 | `partial —` |
| happy-dom-…-intersectionobserver | **did not grade** | — | — | $0.466 | 5404s | 2 | 7 | — | 0 / 3 | — (`settled: false`, wall) |

## The reading works, on two runners, and it goes behind the project's spelling

| run | runner | declared | what it actually ran | exit | named before → after |
| --- | --- | --- | --- | --- | --- |
| ofetch | vitest | `pnpm test` | `pnpm exec vitest run --coverage --reporter=json` | 0 | 28 → 56 |
| igel | pytest | `make test` | `poetry run pytest -rA` | 1 | 2 → 41 (43 by round 3) |

Both take the baseline once and mark it `inherited: true` in every later round —
igel carries it into child nodes `task-2-x1-n1` and `task-2-x1-n2` as well, so
mechanism (f) holds across a whole job tree, not just one leaf's repairs. Red
stays flat in both, so no regression finding was due and none fired.

## The first coverage finding in the benchmark

ofetch round 1, `run.log:41`, from a gate carrying `exercises: 54 rows, 18 unmapped`:

> The request asks for behaviours that no check exercises. Nothing in this
> project's own verification would fail if each of these were absent or wrong…
> `no check exercises: When circuitBreaker: true, defaults are threshold = 5`
> `no check exercises: … cooldown = 30000`
> `no check exercises: … halfOpenMaxRequests = 1`
> `no check exercises: … failureStatusCodes = [408, 409, 425, 429, 500, …]`
> `no check exercises: Request option circuitBreaker accepts true`
> `no check exercises: Behavior must work consistently for $fetch`
> **And 10 more. Write the check for each, and make it pass.**

The finding is **correct**: the run added 28 tests (roster 28 → 56) and none of
them exercised the defaults, which is exactly where the hidden suite still fails
it at 41/47.

**Whether the round closed it is not recorded.** Rounds 2, 3 and 4 carry zero
`exercises` rows — the mapping ran once and the coverage question was never asked
again. igel is the mirror image: its round-1 gate carried `17 rows, 2 unmapped`
and the coverage finding never reached the stream at all, because a different gap
won the verdict.

## Where it still breaks

**The exit-0 door moved to a plain `pass: true`.** ink (13/25) and igel (23/24)
both exit 0 because, after a failing round-1 gate, a later gate returns a bare
passing verdict. Not `PolishClosed`, not `Overturned` — the gate simply accepted
a deliverable the hidden suite fails.

**Two projects could not be read, and it is one cause on two runners.**

| run | runner | command | why |
| --- | --- | --- | --- |
| ink | ava | `npx ava --tap` | killed at its ceiling of 1m53s without finishing |
| happy-dom | vitest | `npx vitest run --reporter=json` | killed at its ceiling of 1m53s without finishing |

happy-dom's is the answer to the monorepo question: the reader **did** go behind
the turbo lifecycle script to `npx vitest` directly. It is the **113-second
ceiling**, not the project shape, and every container here is emulated under
qemu — so this blind spot is part harness and part host.

**textual's journal was empty.** The planner failed
(`fan-out stage 3: the model stopped answering: context deadline exceeded`), the
new fallback caught it (`the planner could not lay this out — running it as one
piece of work`) and the run still produced a 17,300-byte patch — but the store
holds **zero transcript rows, zero acceptance events and zero readings**. Work
happened and nothing about how was recorded. It scored 2/20, down from 18/20.

**One shaped repair, finally observed:** ink `run.log:4`,
`↻ plan: answer cut at the ceiling — continued`. The seam continued a cut plan
rather than losing the run — the failure that killed textual in s4.

## A rig defect this sweep exposed

happy-dom s7 ran the full 90 minutes, spent $0.466 across 7 nodes with 3 restarts
of `task-2`, and produced a **0-byte `model.patch`**, so it could not be graded.
The rig takes the diff as `git add -A && git diff --cached <base_commit>`, which
reads the index. A run that commits its work to a branch and leaves the worktree
back on the default branch therefore grades as though it wrote nothing. The
corpus's own collect command has the same shape, so this is inherited rather than
invented — but it is a hole, and a run that hits the wall mid-branch falls into
it. **Fixed** (`bench/deepswe/run.sh`). The extraction now measures every place the
work could be and takes the richest: the worktree with untracked files staged,
each local branch that moved off base, and HEAD for a detached checkout. They are
candidates rather than a union because the grader applies ONE patch and two
overlapping patches do not apply; every candidate's byte size is written to
`meta.json` as `patch_candidates` with the winner in `patch_source`, so the
choice is auditable and a run whose work was split across two of them is visible
rather than silently halved.

Proved on a constructed repository reproducing the exact failure — work committed
to `feature/work` including a binary file, worktree returned to `main`:

| | old (`git add -A && git diff --cached <base>`) | new (candidates) |
| --- | --- | --- |
| bytes | **0** | **387** |
| files | none | `blob.bin`, `branch_only.py` |
| binary hunk | — | present |
| `git apply --binary --check` on a clean base tree | — | **applies cleanly** |

A plain dirty worktree with no branch work still selects `worktree.patch`, so the
normal case is unchanged.

**happy-dom s7 is LOST, not regraded.** Its container was already removed when the
empty patch was noticed, so there is no workspace left to re-extract from. It is
reported as ungraded — never as a zero.

---

# s8 — the coverage loop closes, and the ceiling stops being silent (`6a9a5a53`)

| task | reward | f2p | p2p | cost | wall | exit | nodes | readings (read/total) | scope | unexercised open→close | `leaf_exhausted` | last line |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| igel-persist-feature-schema | 0 | **22/24** | 2/2 | $0.274 | 3190s | **2** | 7 | **10 / 10** | touched (1 file) | **4 → 0** | 9 | `partial —` |
| ofetch-per-origin-circuit-breaker | 0 | **42/47** | 13/13 | $0.462 | 2644s | **2** | 37 | **8 / 9** | touched (1 file) + whole | **18 → 15** | 32 | `partial —` |
| happy-dom-…-intersectionobserver | 0 | **12/14** | 9/9 | $0.162 | 1803s | **2** | 1 | 0 / 2 | whole | — | 1 | `partial —` |
| textual-richlog-follow-state | 0 | 10/20 | 6/6 | $0.093 | 1205s | **2** | 6 | 0 / 1 | touched (40 files) | — | 6 | — (cancelled parts) |
| ink-grid-box-layout | 0 | 9/25 | 49/49 | **$0.000\*** | 1037s | **1** | 1 | 1 / 1 | whole | — | 1 | — (abandoned) |

\* the ledger, not the truth — see below.

**Five of five exit non-zero.** No run in this sweep claimed to be whole.

## The coverage loop closed for the first time

**igel: 4 unexercised → 0.** Round 1's gate named four behaviours no check
exercised; round 2's gate carries an empty set. The stream says it plainly at
`run.log:47` and the run ends:

> `partial — gate: feature_schema.joblib, description.json (not repaired: no more work could be started on it)`

**ofetch: 18 → 15**, three genuinely closed — `Count a circuit failure for
response statuses listed in failureStatusCodes`, `Non-listed 4xx/5xx … must not
increment`, `One external call is one logical request`. The open set now persists
across rounds as its own `unexercised` array on the gate event, and
`no check exercises` is its own journaled line rather than buried in a gap:

```
L60   22m48s  no check exercises — Circuit breaker is opt-in per origin … — and 17 more
L194  44m3s   no check exercises — Circuit breaker is opt-in per origin … — and 14 more
```

ofetch's findings also drove the work: **37 nodes**, one leaf per unexercised
behaviour, titled verbatim from the points (`when circuitBreaker: true, …`,
`behavior works consistently…`). That is where its $0.462 went.

## Scoping is implemented and mis-aimed in both directions

| run | scope chosen | what it ran | named | verdict |
| --- | --- | --- | --- | --- |
| ofetch | touched packages (1 file) | `pnpm exec vitest run --reporter=json test/circuit-breaker.test.ts` | 30, red 6 → red 0 | **right** |
| igel | touched packages (1 file) | `python3 -m pytest -rA tests/test_igel/test_igel.py` | 2, red 2 → red 0 | **too narrow** — never sees the ~40 tests the run wrote |
| textual | touched packages (**40 files**) | `pytest … tests/animations/ tests/command_palette/ tests/css/ tests/input/ …` | 0 | **too wide** — killed at 1m53s |
| happy-dom | whole | `npx vitest run --reporter=json` | 0 | **not scoped at all** — never found `packages/happy-dom` |
| ink | whole | `npx ava --tap` | **44, partial** | cut, roster kept |

ink is the one clear win of the partial-roster change: cut at the same 112.6s,
`read: true, named: 44, partial: true` where s7 recorded `read: false, named: 0`.

## Unreadable no longer ships as whole

happy-dom's single gate:

```
pass: false   polish_closed: true   unreadable: true
unmeasured: "nothing in this project's verification could be read: `npx vitest
             run --reporter=json` was killed at its ceiling of 1m53s …"
```

`polish_closed: true` is the exact door that let ofetch and ink out at exit 0 in
s6. `unreadable: true` now overrides it → `partial —` line, exit 2.

## Exhaustion is journaled on three axes; resumption is not

49 `leaf_exhausted` events across the sweep, and the reason is legible:

```
⏳ Log API      — it was still working when it ran out of its tokens — 25 turns in
⏳ Synthesis    — it was still working when it ran out of its tokens — 20 turns in
⏳ Happy DOM …  — it was still working when it ran out of its 15m0s — 108 turns in
```

Leaves now stop on tokens (8–28 turns) or on a 15-minute budget, and s6's silent
22-minute re-claim is gone. **But `↻ … resumed from N recorded turns` appears in
not one run of the five.** The half that ends a leaf landed; the half that brings
it back has not been observed.

## Where it still broke

**ink s8 lost its run to a hung call, and then lost its ledger.**

```
16m44s  still waiting: 0 tasks pending, 1 running · last call deepseek/deepseek-v4-flash 16m44s ago
17m17s  ⏳ CSS Grid layout support — the worker did not come back within 17m0s and was given up on
17m17s  ✗ CSS Grid layout support
        deliverable: "executor did not return within 17m0s; abandoned"
```

`leaf_exhausted` then `node_failed`, no gate ever ran, exit 1. The claim-by-
evidence-of-life change converted s6's silent re-claim into an honest give-up,
but nothing retried the hung call — the liveness defect from s1 is still the root
cause. And **`cost.json` reports $0.000228 across one usage row for 17 minutes of
work that left a 26,248-byte patch**: usage is banked on turn completion, so an
abandoned leaf's spend disappears. Any cost figure for a run that ends this way
is fiction.

**textual's two cancelled parts are why it exits 2**, not its gate — the gate
`refused — what it asked for is already on disk under the name the request used`
with `overturned: true` would have acquitted it, but `task-2-x1-n3` and
`task-2-x1-n4` are cancelled and `deliveredWhole` fails on a cancelled part.

## s9 — chat-v3-fix c32f9437 (tool-call bounds, requeue+resume, per-call usage banking, structural adjacency, retaken smaller readings)

| task | f2p | p2p | reward | exit | $ | wall | nodes | usage rows | transcript |
|---|---|---|---|---|---|---|---|---|---|
| happy-dom-deterministic-intersectionobserver | 12/14 | 9/9 | 0 | 2 | 0.203 | 3665s | 4 | 293 | 540 |
| ofetch-per-origin-circuit-breaker | 42/47 | 12/13 | 0 | 2 | 0.078 | 902s | 4 | 137 | 118 |
| textual-richlog-follow-state | 11/20 | 6/6 | 0 | 2 | 0.292 | 2339s | 12 | 410 | 870 |
| igel-persist-feature-schema | 6/24 | 2/2 | 0 | 2 | 0.115 | 1537s | 4 | 161 | 0 |
| ink-grid-box-layout | 1/25 | 49/49 | 0 | 2 | 0.046 | 477s | 2 | 57 | 0 |

**Every run exited 2.** No run reached a `pass: true` or an `overturned` gate, so no run took the exit-0 door
that s5/s7 took. This is the first sweep where the exit code is honest on all five.

### Per-call usage banking landed
All five runs carry a `usage_recorded` row per model call (57–410 rows) rather than one row per node. Cost is
now attributable to the call. happy-dom banks 293 rows for 4 nodes; textual 410 for 12.

### Worker kind (nodes.subharness) — recorded on three of five
- happy-dom: `root`(blank) `task-2`=**bare** `task-2-x1`=**bare** `task-2-x2`=**linear** `task-2-x3`=**linear**
- ofetch: `root`(blank) `task-2`(blank) `task-2-x1`=**bare** `task-2-x2`=**linear** `task-2-x3`=**linear**
- textual: 13 nodes, mixed — `task-2`,`-x1-n1`,`-x1-n2`,`-x2-n1..n5`=**bare**; `-x1`,`-x1-n3`,`-x1-n4`,`-x2`=**linear**
- igel: **all five nodes blank**, transcript rows = 0
- ink: **both nodes blank**, transcript rows = 0

The correlation holds exactly: a node with a recorded worker produces transcript rows; a blank node produces
none. igel and ink wrote 0 transcript rows across 218 model calls — nothing banked, nothing resumable.

### Resumes: zero, on all five
Every `↻` line in s9 is a **worker escalation**, not a transcript resume:
- happy-dom `run.log:80` `↻ Restore methods — escalated linear → bare: escalated from linear after a failed attempt 31m55s`
- ofetch `run.log:31` `↻ Fix test mock — handed to bare: escalated from linear after a failed attempt 8m5s`
- textual `run.log:19` `↻ core-features — handed to bare …`, `run.log:118` `↻ commit-and-verify — escalated linear → bare …`

No run logged `resumed from N recorded turns`. **The requeue+resume path did not fire in s9.**

Verified against the coordinator's challenge: textual's two `leaf_exhausted` events sit on `task-2` (**bare**,
bound=budget, turns=22) and `task-2-x2-n5` (**bare**, bound=budget, turns=13). Both continuations
(`task-2-x1-n1`, `task-2-x1-n2`) opened at **turn 1** with cold repo exploration. The only link is
`job_growth {lineage: task-2, round: 1, adding: 5, allowed: true, reason: gap}` + `subtree_spliced` — no
resume and no overrun event. A **bare** leaf exhausted and its continuation still started from scratch.

### Exhaustion meters — not yet named
s9's `leaf_exhausted` payloads carry `bound` but no `meter`/`reached`/`allowance` (that lands in s10):
- happy-dom: `task-2` att1 **deadline** (15m0s, 97 turns); `-x1` att1 budget/14; `-x2` att1 budget/13, att2 budget/12
- igel: 8 exhaustions, all budget (22,24,16,14,14,11,14,14 turns) — two attempts on every one of its four nodes
- ink: 3, all budget (17,16,13)
- ofetch: 3, all budget (17,15,14)
- textual: 2, both budget (22,13)

**No tool-call cut fired anywhere** — 0 `cut after Ns` lines in all five run.logs.

### Readings
- happy-dom: **8 events**, all `scope: touched packages (1 file)`, `package: packages/happy-dom`,
  `npx vitest run --reporter=json test/intersection-observer/IntersectionObserver.test.ts`, `read: true`,
  `format: node-json`. before=4 named / after=35 named. One after-reading is **red 3** (task-2 first pass),
  the rest green. This is the first run in the benchmark where the before/after pair actually moved
  (4 → 35 named) and the runner parsed.
- ofetch: **4 events**, `touched packages (3 files)`, named 50 before / 50, 50, **49** after.
- textual: **18 events**, all `touched packages (2 files)`, named **3** every time — the reading is aimed at
  2 of the 4 relevant test files, so the named count never moves.
- igel, ink: **0 verification events**. Nothing was read; the gate had nothing to stand on.

### Regression findings — two, both real, first in the benchmark
1. **ofetch** `task-2-x1`: `gate: fail — This work removed checks that existed before it: debug debug request
   object.` Confirmed by the grader: p2p **12/13** (the only p2p regression in the sweep) and by the roster
   itself, `named: 50 → 49`.
2. **happy-dom** `task-2` and `task-2-x1`: `This work removed checks that existed before it: IntersectionObserver
   disconnect() Does nothing, … observe() Does nothing, … takeRecords() Returns empty array, … unobserve() Does
   nothing.` The model deleted the four stub tests when it replaced the stub implementation. The gate caught it
   and grew a repair round (`job_growth round 2 adding 1`), and the repair **closed** it: p2p ended **9/9**, and
   the after-reading went from red 3 to red 0. This is the first repair round in ten sweeps that demonstrably
   fixed what the gate cited.

happy-dom's 12/14 f2p is its best result of the benchmark; it lost the reward on two hidden f2p tests only.

### Unexercised — textual only, and it never closed
textual's three gates each carry the same `unexercised` set of **2 behaviour groups** (5 RichLog behaviours +
8 Log behaviours). Opening 2 → round 2: 2 → round 3: 2. The repair briefs did not carry them: of the 14
`node_briefed` payloads, **13 name 0 of the 2** behaviour groups and one names 1. The set is remembered in the
gate but is not reaching the worker that is supposed to close it.

igel, ink, ofetch, happy-dom gates carry **no** `unexercised` field at all.

### Where each run broke
- **happy-dom** — model. Harness worked: it read, it caught a real regression, it repaired it, it exited 2.
  Two hidden f2p behaviours were never implemented.
- **ofetch** — model, with a harness assist that was ignored. The gate named the deleted check; the repair
  round produced a report instead of a fix (`gap: The deliverable does not contain the actual implementation`),
  and the third round was refused on `the same words were already worked on once`.
- **textual** — harness. 12 nodes, 410 calls, $0.292 (the sweep's most expensive) for 11/20, with the
  unexercised set open the whole way and the readings aimed at the wrong 2 files.
- **igel / ink** — harness, hard. Zero transcript rows, zero readings, every leaf exhausted on budget twice.
  The gate had no evidence to judge and refused on `no more work could be started on it` (igel) and
  `what the review asked for next is not in the request` (ink).

## s10 — chat-v3-fix 054eac8a (named meters, resume, scoped readings)

| task | f2p | p2p | exit | $ | wall | nodes | usage | transcript | settled |
|---|---|---|---|---|---|---|---|---|---|
| ink-grid-box-layout | 17/25 | 49/49 | 2 | 0.538 | 5402s (wall) | 9 | 456 | 1310 | **false** |
| happy-dom | 11/14 | 9/9 | 2 | 0.135 | 5402s (wall) | 23 | 245 | 608 | **false** |
| igel | 6/24 | 2/2 | 2 | 0.244 | 3555s | 7 | 243 | 740 | true |
| ofetch | 42/47 | 13/13 | **0** | 0.070 | 687s | 2 | 62 | 203 | true |
| textual | 5/20 | 6/6 | **0** | 0.040 | 583s | 1 | 69 | 194 | true |

### What landed
- **Meters are named.** `leaf_exhausted` now carries `meter`, `reached`, `allowance`, `unit` — e.g. ink
  `task-2-x2` att1 `meter=cost 238417/186818 turns=29`, happy-dom `task-2` att1 `meter=deadline 854/900 turns=78`.
- **Resume fired for the first time in the benchmark.** ofetch `run.log:34`
  `↻ body-read errors — resumed from 19 recorded turns, already holding circuit-breaker.ts, fetch.ts, types.ts and 1 more`.
  ink resumed 3× (85, 105, 27 turns), happy-dom 2× (78, 49 turns). `leaf_resumed` is now an event kind.
- **First closing unexercised sequence**: igel 4 → 1 → 1 → 0.

### What is still broken
- **Two runs still exit 0 on a failing tree.** ofetch exits 0 on 42/47 with a bare `{'pass': True}` gate;
  textual exits 0 on 5/20 via `refused — what it asked for is already on disk under the name the request used`
  with `overturned: true`. Both had 5 and 15 hidden tests failing respectively.
- **ink and happy-dom both burned the full 5400s wall and never settled** — `settled: false`, **0 delivery_gate
  events**. Whatever the gate would have said was never reached. ink spent $0.538, the most expensive run of the
  benchmark, on 8 exhausted leaves across 9 nodes.
- **Readings go blind.** All ten of ink's finished-tree readings are `named=0 red=0 read=False` — the after
  photograph never parsed. happy-dom's are worse: 3 pairs at `named=4 → 0`, then 5 readings at `scope=whole
  named=0` with `read=False`. A gate reading `named: 0` has no evidence to stand on.
- **textual's scoped reading is aimed at the wrong files.** `scope: touched packages (2 files)` running
  `python3 -m pytest -rA tests/test_concurrency.py tests/test_textlog.py`, `named: 3` — missing `test_log.py`
  and `test_rich_log.py`, the two files the task is actually about. (Fixed in s11.)
- **0 tool-call cuts** on all five.

### Rig note — "richest candidate wins" needs a build-artifact filter
happy-dom s10 selected `worktree.patch` at **335,163,227 bytes** (335 MB). All three other candidates were 0.
The model had been editing compiled output — its own resume line reads `already holding PropertySymbol.d.ts,
PropertySymbol.d.ts.map, PropertySymbol.js and 73 more` — so the worktree diff swept the entire build tree.
It still applied and graded 11/14, but byte-size as the tie-break will pick a build-output diff over a clean
source diff whenever both exist. ink s10 shows the extraction working as intended: `head` and `branch-grid-layout`
both 0, `worktree` 58110 → 17/25.

## s11 — chat-v3-fix 5de2f073

| task | f2p | p2p | exit | $ | wall | nodes | usage | transcript | resumes |
|---|---|---|---|---|---|---|---|---|---|
| textual | **17/20** | 6/6 | 2 | 0.262 | 2679s | 6 | 311 | 920 | 0 |
| happy-dom | 12/14 | 9/9 | 2 | 0.181 | 3438s | 4 | 201 | 582 | 3 |
| ofetch | 42/47 | 13/13 | 2 | 0.144 | 2144s | 4 | 163 | 487 | 3 |
| igel | **0/24** | 2/2 | 2 | 0.302 | 4422s | 4 | 334 | 1057 | 3 |
| ink | — | — | — | — | still running | — | — | — | — |

### What landed
- **Every graded run exits 2.** The `overturned` door is closed: textual `task-2-x1` and igel `task-2-x3` both
  carry `overturned: true` on `what it asked for is already on disk under the name the request used` — the exact
  ground that took textual s10 to exit 0 — and both runs still exit 2.
- **Resume is routine**: 3 resumes each on ofetch, happy-dom, igel, all `resumed_with_transcript: true` and all
  naming banked turns and held files (e.g. igel `↻ Add missing checks — resumed from 50 recorded turns, already
  holding configs.py, feature_schema.py, igel.py and 16 more`).
- **Transcript banking is no longer gated on worker routing.** ofetch s11 has `nodes.subharness` **blank on all
  five nodes** and still banked 487 transcript rows and resumed 3×. Through s9/s10 a blank node meant 0 rows.
- **Readings are aimed.** textual's finished-tree readings expand from `(2 files) named 3` to `(18 files)
  named 282` and `(17 files) named 305`, pulling in `tests/css/`. `named` on happy-dom reaches 377/381.
- **Unexercised closes on 3 of 4**: textual 4→0, ofetch 26→13→9→7, happy-dom 0→0→2→2 (wrong way), igel 4→4→4→4.

### igel s11 — 0/24, the worst result of the benchmark, and the most instructive

**Every one of the 24 hidden f2p tests fails at collection, not at assertion:**
```
E  AttributeError: <class 'igel.igel.Igel'> has no attribute 'results_path'
ERROR tests/test_igel/test_feature_schema_persistence.py::test_fit_persists_feature_schema_and_description_metadata
```
`monkeypatch.setattr(Igel, "results_path", results_dir)` in the hidden fixture `challenge_paths` raises for all
24. p2p is 2/2 — the project's own two tests never touch that attribute.

- **The patch is not the problem.** `[verifier] model.patch applied (76403 bytes)`, `patch_source: worktree.patch`,
  candidates `feature_schema-complete: 76403`, `-missing-tests: 69688`, `-tests: 35792`, `-persistence: 19792`,
  `master: 0`, `head: 76403`. Extraction picked the richest and it applied cleanly. **Rig: clean.**
- **No regression finding fired.** None of the four gates contains `This work removed checks that existed before
  it`. The gates all complain about deliverable *shape* (`The deliverable reports on the work rather than carrying
  it`), and `task-2` did notice the missing artifact — `The request asked for feature_schema.joblib to be written
  in the results directory after fit, but nothing of that name is among what was left behind` — but nothing
  blocked on it.
- **Why the photograph missed it: the regression photograph is over *checks*, not over *symbols*.** The reading
  is `scope: whole`, `poetry run pytest -rA` — not a scoping miss, it ran everything the project has. The named
  roster only ever **grew**: 2 → 6 → 10 → 14. A removed-check finding needs the roster to shrink. `Igel.results_path`
  is a **source attribute**, and no project-owned test exercises it, so deleting it turned nothing red.
- **The baseline was already red and the run looks like an improvement.** Every `before the job's first change`
  reading is `named=2 red=2 exit=1` — the project's own suite fails at baseline. The final reading is
  `named=14 red=0 exit=0`. By the harness's own photograph this run took a red tree green while deleting the
  attribute all 24 hidden tests depend on.
- One reading was unreadable: `named=0 red=0 read=False exit=0`.
- Meters: `task-2-x1` att2 `budget/cost 177066/173184 turns=32`; `task-2-x2` att1 `budget/cost 171821/169726 turns=38`.
- Workers: `root`(blank) `task-2`=bare `task-2-x1`=linear `task-2-x2`=bare `task-2-x3`=linear. 0 tool-call cuts.

**Layer: MODEL deleted the attribute; HARNESS has no mechanism that could have caught it.** The gap is a
symbol-level regression photograph — public attributes/methods present at base and absent at delivery — to sit
alongside the check-level one. Nothing in the check roster can see a deletion that no check covers.

## Why ink s10 and happy-dom s10 burned the whole wall with zero gates

Both ran 5401s to the wall with `settled: false`. A gate is only cut at settlement, so a run the wall kills
mid-round produces **no gate at all** — the 0 gate events are a consequence of never settling, not of a gate
that passed something through.

The question was whether the re-driving was exhaustion→resume of one lineage (which the growth governor does
not count as a fruitless round) or real splits. **The two runs answer differently.**

### ink s10 — every round was an overrun, and the fixed-point detector never got a chance
All **5** `job_growth` events are `reason: overrun`. Not one `gap`, not one `revision`.

| # | node | growth | round | allowed | remainder | produced |
|---|---|---|---|---|---|---|
| 1 | task-2 | overrun | 1 | true | `9c6637ca` | 15 |
| 2 | task-2-x1 | overrun | 2 | true | `d0ef521a` | 5 |
| 3 | task-2-x2 | overrun | 3 | true | `f804b0fb` | 9 |
| 4 | task-2-x3 | overrun | 4 | **false** | `18a30ad8` | 4 |
| 5 | task-2-x3 | overrun | 4 | **false** | `64ae4b6d` | 4 |

Interleaved with the meters:
`task-2` deadline 888/900 t85 → resume `task-2-x1` (85 turns banked) → `task-2-x1` cost 162084/157718 t21 →
`task-2-x1` deadline 872/900 t105 → resume `task-2-x2` (105 turns) → `task-2-x2` cost 238417/186818 t29 →
`task-2-x2` cost 189394/186818 t27 → resume `task-2-x3` (27 turns) → `task-2-x3-n1` deadline 880/900 t108 →
`task-2-x3-n2` cost 185833/180692 t30 → `task-2-x3-n2` cost 182703/180692 t22.

**Eight exhaustions, three resumes, one continuous lineage, and the standstill detector never fired.** It could
not: the remainder hash changed on every single round (`9c6637ca` → `d0ef521a` → `f804b0fb` → `18a30ad8` →
`64ae4b6d`), so the fixed-point test saw motion each time and the governor kept allowing. What finally stopped
it was the **round cap**, not standstill: `cause: rounds — this work has split as many times as splitting helps`,
and by then the wall was gone.

The remainder kept moving because the model kept writing *new scratch files* — the resume payloads name
`debug-grid.ts, debug-grid10.ts, debug-grid11.ts, debug-grid2.ts, debug-grid3.tsx, debug-yoga.ts, debug-yoga2.ts,
debug-test2.tsx, debug-test3.tsx, debug-grid-pos.tsx …`. Every debug file is a change, so every round looked
productive to a detector that asks "did anything change" rather than "did anything *relevant* change".

**Route: the governor counts overrun rounds as free.** Eight exhaustions of one lineage cost the whole wall and
never consumed a fruitless round. An overrun round that resumes the same lineage needs to be counted against
something — a per-lineage overrun budget, or a remainder computed over files the task named rather than over
every path touched.

### happy-dom s10 — standstill DID fire, and a sibling lineage carried on regardless
Growth reasons here are mixed: **3 overrun, 3 revision**.

`task-2` deadline 854/900 t78 → `overrun` round 1 (adding 7, remainder `af471162`, produced 76) → resume
`task-2-x1` (78 turns) → `task-2-x1-n1` deadline 897/900 t49 → `revision` round 1 (adding 7) → `overrun`
round 2 (adding 3, `7211f8bb`) → resume `task-2-x2` (49 turns) → `task-2-x2-n1` deadline 896/900 t30 →
`revision` round 1 (adding 3) → **`task-2-x1` overrun round 3 REFUSED, `cause: standstill`** —

> carrying on has stopped changing anything — twice over now, nothing was written or altered — so this is handed over as it stands

— and then `task-2-x2` `revision` round 2 (adding 2) **continues anyway**, because the standstill refusal bound
only the `task-2-x1` lineage. The run kept going on a sibling until the wall.

**Route: the standstill refusal is per-lineage, but the wall is global.** One lineage being declared a fixed
point says nothing to its sibling, so a run can be refused for standstill and still spend 40 more minutes.

Two other things wrecked this run, both worth separating from the governor:
- **The environment was destroyed by the run itself.** `node_cancelled task-2-x2-n2: revision: Node 1 (clean
  install) did not actually produce a working node_modules with vitest — it only produced a note about trying a
  different approach. The verification step cannot succeed`. That is why every reading is `named=0` and five are
  `read=False`: there was no test runner left to read. The model then edited compiled output
  (`lib/PropertySymbol.d.ts`, `.d.ts.map`, `.js`), which is where the 335 MB patch came from.
- **One transport failure**, not a model refusal: `node_failed task-2-x2-n4: execute request: Post
  "https://openrouter.ai/api/v1/chat/completions": context deadline exceeded`.

### ink s11 (5de2f073) repeats the shape, partly
Wall again (5401s), `settled: false` again, 10/25 f2p, 49/49 p2p, exit 2, $0.459, 12 nodes, 387 usage rows.
What changed: growth is no longer all-overrun (`gap` 1, `overrun` 2, `revision` 1), and **1 gate was cut**
before the wall (`task-2`, `pass: false`, ue=6) where s10 cut none. Resumes dropped 3 → 1. Meters:
`task-2-x1-n1` deadline 870/900 t80, `task-2-x2` cost 162966/157716 t21, `task-2-x1-n2` deadline 850/900 t58.

**The reading blindness on ink is unchanged and is now the standing defect on this task**: every single
`on the finished tree` reading in both s10 and s11 is `named=0 red=0 read=False`, against a baseline that reads
fine (`named=44`, later `named=156 red=2`). ink's after-photograph has never once parsed in either sweep.

## n1 — nvidia/nemotron-3.5-lightning on 5de2f073 (second model, same binary as s11)

| task | f2p | p2p | reward | exit | settled | $ | wall | nodes | calls | $/call |
|---|---|---|---|---|---|---|---|---|---|---|
| happy-dom | 10/14 | 9/9 | 0 | 2 | true | 0.511 | 3951s | 4 | 375 | 0.001364 |
| igel | 5/24 | 2/2 | 0 | 2 | **false** (wall) | **2.057** | 5402s | 3 | 968 | 0.002125 |
| textual | 4/20 | 6/6 | 0 | 2 | true | 0.464 | 1699s | 3 | 280 | 0.001655 |
| ofetch | **0/47** | **5/13** | 0 | 2 | true | 0.853 | 2774s | 4 | 454 | 0.001880 |
| ink | **0/25** | 49/49 | 0 | 2 | **false** (wall) | 1.305 | 5402s | 4 | 777 | 0.001680 |

**Sweep cost $5.19 — over the $5 kill threshold.** Every nemotron run cost 1.5–2.5× the deepseek run on the
same task, and igel alone cost $2.06 (968 calls) against deepseek igel s11's $0.302 (334 calls). The per-call
price is the whole story: nemotron averages $0.00168/call against deepseek's ~$0.00085.

### MODEL defects — nemotron-specific, all three kinds the harness names
1. **Structured-output failures.** happy-dom fired both repair kinds inside 25s:
   `{"lane":"compile","kind":"continued","spent":451,"ceiling":9219,"line":"compile: answer cut at the ceiling — continued"}`
   and `{"lane":"compile","kind":"reasked","line":"compile: the answer was not readable — asked again"}`.
   Deepseek never triggered either in eleven sweeps. **The harness recovered both times.**
2. **Planner fallback on 3 of 5 runs** (ofetch, happy-dom, igel):
   `{"lane":"plan","kind":"fell back","line":"the planner could not lay this out — running it as one piece of work"}`.
   Nemotron could not emit a usable plan graph. The fallback is the harness handling it correctly.
3. **Deliverable shape.** textual `task-2-x2`: `The deliverable is a single JSON contract string, not the
   required Python source files.` It answered the delivery prompt with an object instead of the artifact.
4. **Scratch churn**, same failure family as deepseek's ink: igel's resume payloads name `igel.py.bak,
   igel.py.bak2`; ink's name 72 compiled `.d.ts`/`.js`/`.js.map` carriers. igel ran a **262-turn** attempt.

### HARNESS behaviour — mostly correct, two defects worth routing
- **ofetch: the regression photograph worked perfectly.** The model broke 8 passing tests (p2p 5/13) and the
  gate named them: `This work broke checks that were passing before it: ofetch calls hooks, ofetch hook errors`.
  The readings tracked it exactly — baseline `whole named=28 red=0` every time, finished-tree collapsing to
  `named=1 red=1` four times, then recovering to `named=28` with red 2/7/2. This is the mechanism igel s11
  needed and could not have, because there the loss was a bare symbol rather than a check.
- **DEFECT (known, pre-fix): own-checks counted as regressions.** happy-dom's gate says `This work broke checks
  that were passing before it: IntersectionObserver initial observation queuing Queues an entr…` while **p2p is
  9/9**. Baseline roster named 4; the delivery roster named 33, because the model wrote the other 29. The check
  named was never in the baseline roster. Under `9e65f627` this becomes `DeliveryGate.OwnFailing` — *the checks
  this work wrote fail*. Recorded here as the known pre-fix defect, not a new finding.
- **DEFECT: ink's after-photograph still never parses.** Ten finished-tree readings, every one
  `named=0 red=0 read=False`, against a baseline that reads fine (`named=45`). Identical to ink s10 and s11 —
  three sweeps, two models, same blindness. This is a task-level runner defect, not a model effect.
- Resumes fired on all five (igel from 261 and 135 turns; ink from 116, 189, 194). Exit 2 on all five, honest.
- The source-bytes patch rule held: `patch_generated_warning: null` everywhere, and happy-dom — the run that
  produced the 335 MB mis-selection in s10 — selected a clean 25559-byte `worktree.patch` with all three other
  candidates at 0.

### Where each run broke
- **happy-dom 10/14** — MODEL. Harness read, caught, repaired shape twice, exited 2. Four hidden behaviours unmet.
- **igel 5/24** — MODEL + wall. 262-turn attempt, `.bak` churn, never settled; baseline suite red at base
  (`named=2 red=2`) so the photograph had little to stand on.
- **textual 4/20** — MODEL. JSON-contract deliverable, three times. Readings never widened past `(4 files) named 6`.
- **ofetch 0/47** — MODEL, unambiguously. It broke the suite; the harness said so twice and exited 2.
- **ink 0/25** (p2p 49/49) — HARNESS (reading blindness) + MODEL (72 compiled carriers). Wall, `settled: false`.
  Nothing regressed and nothing was achieved: the model never implemented the grid feature, and both gates said so
  (`The deliverable does not implement any of the 6 requested grid display behaviours`, ue=6 on both). With every
  finished-tree reading blind (`named=0 read=False`), the harness could not have told the difference between this
  and a working run from its own evidence.

## s12 — chat-v3-fix 2045193a (governor, gate-judges-the-tree, reading scope, uncollected suites)

| task | f2p | p2p | exit | settled | $ | wall | nodes | calls | $/call | graded | box |
|---|---|---|---|---|---|---|---|---|---|---|---|
| textual | 15/20 | 6/6 | 2 | true | 0.280 | 2498s | 16 | 273 | 0.001026 | 14:32Z | quiet |
| happy-dom | 12/14 | 9/9 | 2 | **false** | 0.307 | 5401s | 12 | 287 | 0.001071 | 15:26Z | contended |
| igel | **0/24** | 2/2 | 2 | **false** | 0.282 | 5402s | 14 | 296 | 0.000952 | 15:50Z | contended |
| ink | 0/25 | 49/49 | 2 | true | 0.258 | 4063s | 5 | 279 | 0.000923 | 15:31Z | contended |
| ofetch | 4/47 | 13/13 | 2 | true | 0.153 | 1686s | 4 | 145 | 0.001053 | 14:19Z | quiet |

**Box contention.** A foreign fleet of ~56 containers appeared ~14:25–14:37Z; 1-min load peaked at 87 against
20 cores. happy-dom, igel and ink graded inside that window and all three show deadline-metered exhaustions
(happy-dom 3 of 4, igel and ink 1 of 4); ofetch and textual finished before it and were purely cost-metered.
Wall-derived numbers for the three contended rows are suspect. No 429s or transport timeouts in any log — the
contention was CPU, not the provider.

**Gate-lane defect (routed).** ofetch and textual carry the full triple: a gate finding about the content of a
record file, a round-1 `goal-already-covered` refusal, and `subject: None`. happy-dom, igel and ink journal
`subject` normally (`tree (191 files)`, `tree (9 files)`, `tree (99 files)`), so the missing subject tracks the
defect exactly. `goal-already-covered` refused four growth rounds on happy-dom and one each on ofetch/textual.
Scratch counters now carry values: happy-dom 53, ink 5, textual 1, igel 1.

**ink's after-photograph is fixed.** 4 of 7 finished-tree readings now parse (`named` 26, 43, 43, and 58 at
red=15) where s10, s11 and n1 were 10/10 blind. Every baseline reading carries `partial: True` — cut rosters
kept — and the 3 still-blind readings now say why:
`` `npx ava --tap` ran on the finished tree and was killed at i… ``. The remaining blindness is a suite that
does not finish, and the reading now records that rather than reporting an empty roster as fact.

### igel s12 — the surface photograph works; a different breakage takes the run to 0

**The surface photograph fires, and it fires on exactly the defect igel s11 could not see.** `task-2` journals
`surface {"compared": 8, "lost": 8, "names": ["Igel.default_dataset_props", "Igel.default_model_path",
"Igel.default_model_props", "Igel.default_onnx_model_path", "Igel.description_file", "Igel.evaluation_file",
"Igel.prediction_file", "Igel.results_path"]}` — including `Igel.results_path`, the single deleted attribute
that cost s11 all 24 tests with nothing in the store to name it. The gate says it in words: `This work removed a
public name that existed before it: Igel.default_dataset_props, …`. A repair round was **bought** (`gap` round 1,
allowed) and it **worked**: `task-2-x1` re-photographs at `{"compared": 1, "lost": 0}`, and every later surface
event on `-x1`, `-x2-n3`, `-x2-n4`, `-x2-n6` reports `lost: 0`. The s11 hole is closed.

**What holds it at 0 is a new and different breakage, and it is neither regression-blindness nor the wrong file.**
All 24 hidden tests now fail on `TypeError: 'Configs' object does not support item assignment` — zero occurrences
of `has no attribute` in the verifier log. The model made `Configs` non-subscriptable. That is a **behaviour
change on a name that was retained**, so the surface photograph cannot see it by construction: it compares the
presence of public names, not their semantics, and `Configs.__setitem__` is not a public name in that set.
The check roster could not see it either — the project's own suite went `named=2` at baseline to `named=54`, with
a persistent `red=2` that never cleared and never grew, because no project-owned test assigns into `Configs`.

**And the run was cut off before it could find out: the settle was `out-of-wall`.** `task-2-x2` round 3 is
`allowed=False, cause=out-of-wall` — the first time that cause has appeared in any sweep — with `settled: false`
at the full 5402s wall, under the foreign-fleet contention. So the answer to "regression, wrong file, or early
settle" is: **not regression** (the surface mechanism did its job and closed its finding), **not the wrong file**
(`igel/igel.py` was correctly named and edited, patch `branch-feature_feature-schema-persistence.patch` applied),
but a **semantic regression invisible to both photographs, plus a wall-truncated settle** that ended the run
while three revision rounds were still in flight.

The mechanism this wants is the assertion door landing in s14: a behaviour is exercised only when a check the run
wrote asserts its observables. Nothing in igel s12 asserted that `Configs` still supports item assignment.

## s13 — chat-v3-fix 40940bed (whole-file judging, HeldPoints, record-file exemption, subject journalled)

| task | f2p | p2p | exit | settled | $ | wall | nodes | calls | $/call | gates |
|---|---|---|---|---|---|---|---|---|---|---|
| textual | **19/20** | 4/6 | 0 | true | 0.132 | 2862s | 7 | 206 | 0.000641 | 2 |
| ofetch | 29/47 | 11/13 | 2 | **false** | 0.559 | 5407s | 33 | 422 | 0.001324 | **0** |
| ink | 17/25 | 49/49 | 2 | true | 0.455 | 4945s | 6 | 346 | 0.001316 | 1 |
| happy-dom | 9/14 | 9/9 | 2 | true | 0.135 | 2663s | 4 | 187 | 0.000723 | 4 |
| igel | 0/24 | 2/2 | 2 | true | 0.235 | 3660s | 10 | 355 | 0.000663 | 2 |

`subject` is journalled on every gate in the sweep — the s12 defect is closed. Readings are clean too:
**0 blind readings across all five runs**, where s12 had 4 on happy-dom and 3 on ink.

### textual s13 — 19/20, closest to reward 1, and the two p2p losses were unseeable
Broken: `test_log_scrolling_updates_visible_viewport_and_scrollbar_position` and its `rich_log` twin
(`AssertionError: assert 0 == 4 … ScrollBar(...position=0...)`). Both live in
`tests/test_rich_log_follow_state.py`, **which does not exist in the agent's repo** — it is injected by
`test.patch` at grade time, and `grep -c test_rich_log_follow_state model.patch` is **0**. Four of the six p2p
tests are in that hidden file; only the two `tests.test_log.*` are project-owned, and both passed. So no roster
at any scope could have held these names, and no regression finding was possible. **Neither a scope nor a
governor fault.** The missing f2p (`test_rich_log_expand_entries_reflow_after_min_width_change`) *is* named by
the gate's unexercised set — *"RichLog.write(expand=True) no longer preserves full-width justified rendering
with current Rich"* — so the harness named the gap it then failed to close.

### ofetch s13 — the zero-gate case
33 nodes, 422 calls, ten exhaustions (7 cost, 1 deadline, 1 `turns`), $0.559, `settled: false` at the 5407s
wall, and **no delivery_gate at all**. Growth ran eight rounds and ended `goal-already-covered`, then **twice
`out-of-wall`**. Its two broken p2p tests are also hidden-file tests (`test/circuit-breaker.test.ts`), and the
surface photograph correctly reported `lost: 0` on all eight events — nothing public was deleted. This is the
run the s15 settlement watch is meant to make impossible.

### happy-dom s13 — four gates, one finding, no progress
All four gates repeat the identical stub-test finding (`This work removed checks that existed before it:
IntersectionObserver disconnect() Does nothing, …`), four `gap` rounds ending `cause: rounds`. Cheapest
happy-dom yet ($0.135/187 calls) because it stopped early rather than working the problem — versus s12's
12/14 over 287 calls. The five remaining challenge behaviours were never implemented.

### igel s13 — surface fires five times, run still scores 0
Five `surface` events all report `lost: 8` naming the same set (`Igel.default_dataset_props … Igel.results_path`).
Two `goal-already-covered` refusals landed on `revision` round 1 — before the record-file exemption could help —
and growth ended `cause: rounds`.

## s14 — mixed binaries: textual/ofetch/happy-dom = 419d6dc9, igel/ink = 58fffb5f

| task | f2p | p2p | exit | settled | $ | wall | nodes | calls | $/call | gates | binary |
|---|---|---|---|---|---|---|---|---|---|---|---|
| happy-dom | **13/14** | 9/9 | 2 | true | 0.061 | 1801s | 4 | 99 | 0.000612 | 4 | 419d6dc9 |
| ofetch | 40/47 | 13/13 | 2 | true | 0.239 | 1865s | 8 | 218 | 0.001096 | 4 | 419d6dc9 |
| igel | 0/24 | 2/2 | 2 | true | 0.236 | 2854s | 20 | 289 | 0.000803 | 2 | 58fffb5f |
| textual | **grade-failed** | — | 2 | true | 0.219 | 3761s | — | — | — | — | 419d6dc9 |
| ink | pending | | | | | | | | | | 58fffb5f |

**textual s14 = `grade-failed` (verifier timeout 1800s: suite hung on `test_example_script_exists_and_boots`).**
`grade_seconds: 1921` against the task's 1800s cap. Not scored, and not regraded: the benchmark's verifier
timeout is part of the benchmark, and a run whose example script hangs the suite scores 0 under DeepSWE.
*Informational only, not a score*: of the 22 tests that completed before the hang, 19 passed and 3 failed.

**happy-dom 13/14 is the best result of the benchmark and the cheapest run in it** ($0.061, 99 calls). The one
miss is `IntersectionObserver > observe() > Detects threshold crossings in subsequent async delivery cycles`,
failing on `Test timed out in 500ms` — a hang, not a wrong value. **Nothing named it**: `unasserted` and
`unexercised` are both absent from all four gates, which instead repeat the same stub-test finding across four
`gap` rounds ending `cause: rounds`. Readings climbed `named 4 → 37 → 41` at `red=0` throughout, so the evidence
pointed nowhere near the missing async cycle.

**ofetch s14 shows three mechanisms working.** `OwnFailing` is live and correctly worded — `The checks this work
wrote fail: circuit breaker failure accounting non-listed status in half-open does not close the circuit. They
were not in the project's roster when …`. `unexercised` carries real behaviour text overlapping the seven
failures. And the HeldPoints retake fired with its note verbatim:
`structured_repair(gate, reasked, note: "parse response: a fail must quote one of the behaviours this…")`.

### igel s14 — the surface reader names the exact cause; the gate never hears it
All 24 hidden tests fail on one line: `ImportError: cannot import name 'temp_post_req_data_path' from
'igel.configs'`. The new module-level Python surface reader **caught it on three nodes**:
`surface task-2-x1-n4 {"compared": 5, "lost": 3, "names": ["init_file_path","res_path","temp_post_req_data_path"]}`
(same on `-n3`, `-n1`). `temp_post_req_data_path` is precisely the name in the ImportError.

But **no `consumers` event exists in the store at all** — `configs` never appeared as a changed definition and
its usage sites were never enumerated, so there is no site count to report — and **no gate grounded on a
consumer site** (`sourced: None`, no `consumers` field on either gate). Only 2 gates were cut, both about the
missing artifact (`gap: feature_schema.joblib`, then `feature_schema.joblib, description.json`), both with
`held_point: 'After fit, write feature_schema.joblib in the results directory'`. The `lost: 3` findings sit on
leaf nodes whose findings never propagated to a judged node.

**Layer: the photograph sees it, the gate never hears it.** Routed to the consumers lane.

**New fields not yet journalled on these binaries** (expected — both predate 338957b0): `replaced` is `None` on
every reading in every s14 run, and `JobGrowth.Finding` is `None` on every growth event. No third-round refusal,
no close-out or forced-judgement events; both multi-gate runs ended on `cause: rounds` at round 4.

# s15 — dev b3922479, both doors (2026-09-04): the chat surface measured beside do for the first time

**Question asked:** "quick bench on chat mode and do mode for current dev with 5–8 DeepSWE — we should be good now on cost, walltime and quality?"

**Answer:** Not yet, on either door. No task reached reward 1 on either door (0/16 cells). On the five tasks with s13/s14 history, today's `do` is better on two (ofetch 43/47 — the best any sweep has reached; igel 6/24 where every sweep before scored 0), level on one (happy-dom 12/14 vs 13/14), and worse on two (ink 13/25 vs 17/25; textual 9/20 vs 19/20 at 8.7× the cost). Cost went the wrong way: the five baseline tasks cost $2.74 on `do` today against $1.52 in s13, with textual ($1.14 vs $0.13) and happy-dom ($0.47 vs $0.06) carrying most of it. `chat` beats `do` on hidden-test count on five of eight tasks (ofetch 46/47, textual 16/20, igel 9/24, happy-dom 13/14, aiomonitor 45/53) and is nearly level on macro average (54.2% vs 55.5%), but at about twice the money (median $0.75 vs $0.32 per cell) and one and a half times the wall (median 3856 s vs 2494 s), and one bad cell (cattrs 3/69 for $1.37) pulls its micro average down to 41.8% against `do`'s 50.4%. Every `do` cell ended `partial` (exit 2); six `chat` cells ended on their own seal and two on the stall detector.

Setup: rig `bench/deepswe/` with the new `DOOR=chat` (PR #634), one linux/amd64 build of dev `b3922479` copied into each task's own amd64 container (Rosetta on an M-series host, 12 CPU / 40 GB Docker VM), model pinned to `deepseek/deepseek-v4-flash` on every seat, 5400 s agent wall on both doors (task.toml v1.1 now says 10800; pinned to match s1–s14), `chat` with `--yolo --one-model --max-hours 1.4833 --max-cost 3`, `do` with `-yes-spend -json -timeout 5400`. Eight tasks: the five of PICKS.md plus cattrs, aiomonitor, ts-pattern; all eight gold-grade to 1 on this rig. Eight lanes ran concurrently, first door then second (order alternated per task), one Codex operator watching each cell; no operator ever had to answer a card (0 interventions in 16 cells — see "asked" below for why). Cells: `results/<task>-deepseek-deepseek-v4-flash-<door>/`; per-task reports by the lanes: `reports/<task>.md`; the lead's running notes: `FINDINGS-NOTES.md`; what changed mid-run and why: `DECISIONS.md`. Total spend on the key: about $10.2 including the smokes and the three restarted cells.

## The table — both doors, per task (dev b3922479, deepseek/deepseek-v4-flash, 5400 s wall)

| task | door | reward | hidden f2p | p2p | cost | wall | calls | nodes/turns | gates | ended | GC |
|---|---|---|---|---|---|---|---|---|---|---|---|
| ofetch | do | 0 | 43/47 | 12/13 | $0.49 | 3329s | 419 | 12 | 3 | partial | 100 |
| ofetch | chat | 0 | 46/47 | 12/13 | $0.41 | 2694s | 319 | 4 |  | self | 100 |
| ink | do | 0 | 13/25 | 49/49 | $0.39 | 3296s | 287 | 7 | 3 | partial | 100 |
| ink | chat | 0 | 9/25 | 49/49 | $0.96 | 4306s | 369 | 3 |  | self | off |
| textual | do | 0 | 9/20 | 4/6 | $1.14 | 5152s | 1250 | 44 | 0 | partial | off |
| textual | chat | 0 | 16/20 | 6/6 | $0.72 | 3794s | 656 | 4 |  | stall | 100 |
| igel | do | 0 | 6/24 | 2/2 | $0.25 | 1693s | 373 | 13 | 3 | partial | 100 |
| igel | chat | 0 | 9/24 | 2/2 | $0.33 | 1754s | 141 | 2 |  | self | off |
| happy-dom | do | 0 | 12/14 | 9/9 | $0.47 | 1342s | 400 | 15 | 2 | partial | off |
| happy-dom | chat | 0 | 13/14 | 9/9 | $0.43 | 4336s | 595 | 3 |  | self | 100 |
| cattrs | do | 0 | 44/69 | 7/7 | $0.22 | 5089s | 322 | 17 | 2 | partial | 100 |
| cattrs | chat | 0 | 3/69 | 7/7 | $1.37 | 3672s | 387 | 3 |  | self | 100 |
| aiomonitor | do | 0 | 43/53 | 8/8 | $0.04 | 536s | 46 | 3 | 1 | partial | off |
| aiomonitor | chat | 0 | 45/53 | 8/8 | $1.13 | 3919s | 471 | 2 |  | stall | 100 |
| ts-pattern | do | 0 | 0/85 | 6/6 | $0.17 | 1341s | 262 | 13 | 3 | partial | 100 |
| ts-pattern | chat | 0 | 0/85 | 6/6 | $0.79 | 5147s | 601 | 6 |  | self | off |

- **do**: reward-1 0/8 graded · total cost $3.16 · total wall 21778s
- **chat**: reward-1 0/8 graded · total cost $6.12 · total wall 29622s
- **do** hidden-test micro pass: 170/337 = 50.4%
- **chat** hidden-test micro pass: 141/337 = 41.8%

## Against s13 and s14 (s13 40940bed · s14 419d6dc9/58fffb5f)

| task | s13 do | s14 do | today do | today chat |
|---|---|---|---|---|
| ofetch | 29/47 · $0.56 · 5407s | 40/47 · $0.24 · 1865s | 43/47 · $0.49 · 3329s | 46/47 · $0.41 · 2694s |
| ink | 17/25 · $0.46 · 4945s | pending in the autopsy | 13/25 · $0.39 · 3296s | 9/25 · $0.96 · 4306s |
| textual | 19/20 · $0.13 · 2862s | grade-failed (verifier timeout; 19/22 of the tests that ran) · $0.22 · 3761s | 9/20 · $1.14 · 5152s | 16/20 · $0.72 · 3794s |
| igel | 0/24 · $0.23 · 3660s | 0/24 · $0.24 · 2854s | 6/24 · $0.25 · 1693s | 9/24 · $0.33 · 1754s |
| happy-dom | 9/14 · $0.14 · 2663s | 13/14 · $0.06 · 1801s | 12/14 · $0.47 · 1342s | 13/14 · $0.43 · 4336s |
| cattrs | — | — | 44/69 · $0.22 · 5089s | 3/69 · $1.37 · 3672s |
| aiomonitor | — | — | 43/53 · $0.04 · 536s | 45/53 · $1.13 · 3919s |
| ts-pattern | — | — | 0/85 · $0.17 · 1341s | 0/85 · $0.79 · 5147s |

## Per-door aggregates (8 tasks each)

| door | reward 1 | hidden-test micro | hidden-test macro | tasks where this door had more hidden passes | regressions (cells with p2p loss) | total cost | median cost | median wall | ended |
|---|---|---|---|---|---|---|---|---|---|
| do | 0/8 | 170/337 = 50.4% | 55.5% | 2 | 2 | $3.16 | $0.32 | 2494s | partial 8 |
| chat | 0/8 | 141/337 = 41.8% | 54.2% | 5 | 1 | $6.12 | $0.75 | 3856s | self 6, stall 2 |

`GC` is the emulation guard: `off` is the rig's `GOGC=off`/`GOMEMLIMIT` qemu guard, which under Rosetta only pinned 6 GiB per process and forced a mid-run switch (DECISIONS.md); `100` is the ordinary collector. It does not change what the product does or how it is graded.

## What the sixteen cells say about the product

**Neither door finishes a task.** Sixteen cells, zero reward-1. The three near misses — ofetch/chat 46/47, ofetch/do 43/47, happy-dom/chat 13/14 — each miss on one behaviour the run never named (ofetch: a p2p test both doors break, `only treats configured failureStatusCodes as status-based failures`; happy-dom: `Detects threshold crossings in subsequent async delivery cycles`, the same test both doors fail and no gate mentioned).

**ts-pattern is a zero on both doors for one reason, and it is the model's contract, not the rig.** The hidden `tests/match-each.test.ts` fails ts-jest type-checking against either patch at the same `.tap((val) => tapped.push(val))` lines — chat typed the callback for `string` where the tests hand `string[]`, do typed it `unknown` and left an unused `@ts-expect-error` — so the suite never runs and all 85 f2p read "test did not run" (p2p 6/6 is `helpers.test.ts`, untouched). One type error takes the whole file down; a TypeScript task is graded on its types.

**`do` on dev b3922479:**
- Every run ends `partial` (exit 2), none `done`. The endings are the gate refusing (ofetch, ink, igel, happy-dom, ts-pattern, cattrs), the no-progress rule (happy-dom at 1342 s), a run stopping with 90% of its wall unused after the gate refused a rule that WAS satisfied (aiomonitor: "work in a new branch from main" — the work was on branch `snapshots`, which is the branch the rig graded), and a wedged run (cattrs: no model call from 17:26 to the wall, `still waiting: 3 tasks pending, none running` every 30 s for an hour, two "Write tests" parts pending with nothing to run them).
- The deliverable contradicts the gate on three runs: happy-dom opens "No changes were needed — the names were never removed" under a gate that caught exactly that removal; igel says "Everything is already done… all 19 tests pass" under `feature_schema.joblib … nothing of that name is among what was left behind`; ts-pattern argues the gate's five gaps are covered by tests the agent wrote itself, which the verifier resets.
- Cost is in the tree: textual grew to 44 nodes / 1250 calls / $1.14 with at least five token-budget exhaustions and two nodes dying on unreadable replies, and landed at 9/20 where s13 got 19/20 from 7 nodes for $0.13; happy-dom 15 nodes / 400 calls / $0.47 for the same 12–13/14 s14 got from 4 nodes for $0.06. Token-budget overruns (`203161 of 173682 tokens of billed work`, `178745 of 150000`) and transcript resumptions (happy-dom 8, ofetch 6, textual ≥5) recur on every long run. ofetch's last 35 minutes were one node restarted 3 times and resumed 6, ending `did not finish — All providers have been ignored` (the router had vetoed every machine behind the model).
- `gate: the answer was not readable — asked again` appears on ofetch (2 of 3 rounds), igel (4, two escalating to "giving up"), ink (2), ts-pattern (2).

**`chat` on dev b3922479:**
- The sidebar counts "N needs you" (up to 3) on ofetch, ink, aiomonitor, happy-dom, ts-pattern and textual, and on none of them did a `needs your look` line or an `[a] accept` card ever appear — so the rig's ask grace never fired, a person watching would have had nothing to answer, and the count outlived the run. Every lane reported it independently.
- The status line reads `idle` for 25–40 minutes at a stretch while a task works underneath it (igel ~25 of 29 min; ink ~40 min with 160 calls and $0.65 going through; happy-dom ~24 min; the cost counter is the only progress signal).
- Tasks end "lost the connection — branch kept" (aiomonitor, textual ×2) or "not accepted — branch kept" (happy-dom ×2: `RootMargin parser`, `Threshold normalizer`) and their branches never reach the scored patch, while the closing summary claims the suite green ("Tests: 45/45 passing", "60/60 local tests").
- Two cells ended `stall` (textual at 3794 s with the rail claiming 7 running; aiomonitor at 3919 s after ~40 min and $0.49 of read-only git exploration, "14 tool calls without visible assistant text") — ten minutes of a silent call log under a screen that says working.
- Self-inflicted setbacks on ts-pattern: a `git stash` of its own uncommitted work ("the task loop is failing because of my uncommitted work") and a git call that died with `unable to start editor 'editor'` because the container has no EDITOR.
- Where it wins, it wins on the same money `do` spends: ofetch 46/47 for $0.41 (do 43/47 for $0.49), happy-dom 13/14 for $0.43 (do 12/14 for $0.47), textual 16/20 clean for $0.72 (do 9/20 with two regressions for $1.14). Where it loses it loses expensively: cattrs 3/69 for $1.37 (do 44/69 for $0.22), ink 9/25 for $0.96 (do 13/25 for $0.39), aiomonitor 45/53 for $1.13 (do 43/53 for $0.04).

## Against the do sweeps (five tasks with history)

| task | best do before today | today do | today chat | read |
|---|---|---|---|---|
| ofetch | 40/47 · 13/13 · $0.24 · 1865 s (s14) | 43/47 · 12/13 · $0.49 · 3329 s | 46/47 · 12/13 · $0.41 · 2694 s | more tests, one regression, 2× cost |
| ink | 17/25 · 49/49 · $0.46 · 4945 s (s13) | 13/25 · 49/49 · $0.39 · 3296 s | 9/25 · 49/49 · $0.96 · 4306 s | fewer tests, cheaper, faster |
| textual | 19/20 · 4/6 · $0.13 · 2862 s (s13) | 9/20 · 4/6 · $1.14 · 5152 s | 16/20 · 6/6 · $0.72 · 3794 s | do regressed hard on tests, cost and wall; chat clean p2p |
| igel | 0/24 (s13, s14; 5/24 s1) | 6/24 (lower bound — wrong branch graded, rig) · $0.25 · 1693 s | 9/24 · $0.33 · 1754 s | first non-zero since s1 on both doors |
| happy-dom | 13/14 · $0.06 · 1801 s (s14) | 12/14 · $0.47 · 1342 s | 13/14 · $0.43 · 4336 s | level on tests, 7–8× cost |

## Rig faults this campaign found and fixed (all on PR #634 or in DECISIONS.md)

1. **A cell started from a Codex tool call dies when the call returns** (process-group kill). Every first cell died that way at 15:23; relaunched detached via `launch.sh` (setsid). Cost: ~15 min, nothing spent.
2. **The qemu emulation guard under Rosetta**: `GOGC=off` pinned every aforge process at its 6 GiB `GOMEMLIMIT`; seven cells held 32 GB of a 40 GB VM. Proved unnecessary under Rosetta (gold reward 1 with normal GC), switched for cells starting after 15:45, three young cells restarted (ofetch/do, cattrs/chat, aiomonitor/chat), a launch gate added. Rows carry the regime in the `GC` column.
3. **Binary hunks counted as source in the candidate pick**: igel/do was graded from a branch made "richest" by a committed 502 KB `model.joblib`, missing `igel/configs.py` and every fixture; its 6/24 is a lower bound and cannot be regraded (candidates lived only in the removed container). Fixed: git's binary markers and a list of binary extensions go to the generated side, and `/bench/candidates` is copied out beside every result.
4. **Bookkeeping**: `cost.json`'s `calls` (store usage rows) and `calllog_calls` differ on every `do` cell (e.g. 329 vs 400) while the dollars agree; textual/do's store cost ($1.144) and call-log cost ($1.082) differ by 6%; chat's `cost.json` carries no token counts. The lead's operator restart at 15:59 (a mistaken duplicate cleanup) interleaved two operators' `monitor.md` on igel/do and lost the chat operator's final message on igel; no cell was affected.

## Where the campaign lives

`~/aforge-benchmarks/deepswe-dev-flash-20260904-b3922479/` on the box that ran it: `results/<task>-deepseek-deepseek-v4-flash-<door>/` per cell (meta, cost, reward, the graded patch, every candidate patch for cells after 16:35 UTC, the run's home with the key scrubbed, and for chat the screen it ended on and the status line every five seconds), `reports/<task>.md` written by the lane that ran the task, `REPORT.md`, `DECISIONS.md`, `FINDINGS-NOTES.md`, `render-report.py` for the tables. The rig is this directory at PR #634 plus `DOOR=chat`; `cell.sh` there pins `AGENT_SECONDS=5400`, `CHAT_CAP=3`, `EMU_GOGC=100`.

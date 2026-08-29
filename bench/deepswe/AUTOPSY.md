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

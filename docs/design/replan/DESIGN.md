# Replanning in the task engine: the design, its critique, and a baseline

*2026-09-23. Branch `task/replan-design-and-baseline`, cut from `santos/dev2` at
`7c3a49978`. Nothing in the engine changes on this branch except one
experiment switch, off by default.*

## 1. Why this document exists

The goal for the task engine, in Santosh's words: "dynamic adaptation as plans
complete, constantly checking whether there are cleverer ways to parallelise
based on results, or to make it more accurate", and "how can we do it without
wasting?"

A replanning layer is easy to design and easy to overbuild. Before anyone
writes one, this document does three things:

1. Describes the engine as it is, with file and line references, so the
   proposal is argued against the real code and not a memory of it.
2. Sets the proposal beside its critique, and says which parts survive.
3. Measures today's engine on four small cells built to hide the structures
   replanning is supposed to exploit, so the first thing built is the thing
   the data says is missing.

Section 8 holds the baseline's numbers and section 9 the conclusion.

## 2. The engine as it is

All references are to `santos/dev2` at `7c3a49978`.

### 2.1 Which door reaches it

- `codeaf do` on the bash belt goes to `runErrand`
  (`cmd/codeaf/do.go:3413`), which calls `run.Start` over the working copy's
  own plan store (`.codeaf/plandb.db`) with the review round on
  (`cmd/codeaf/do.go:3454`).
- A chat `/task` on the bash belt goes to `startTaskRun`
  (`internal/session/task_run_belt.go:251`), but only in a binary that links
  `internal/run`; the link is the blank import in `cmd/codeaf/runwire.go`.
  A binary that does not link it falls back to the legacy node road
  (`task_run_belt.go:254`).

**A finding from reading this:** `bench/bashloop`'s task door builds a session
in its own process and imports `internal/session` but never `internal/run`, so
no engine is registered there and its "bash belt" arm ran the legacy road, not
this engine. The baseline below therefore goes through the `do` door, which
calls the engine directly.

### 2.2 Roles and seats

`SeatFor` (`internal/run/crew.go:51`) maps a task's role to a crew tier:
`plan` to mastermind, `check` to the careful (high) tier, `probe` to the low
tier, and everything else, `work` included, to the worker tier. The door's
`-plan-model` fills the plan seat and, when no `-check-model` is given, the
check seat too (`config.CheckSeat`, called from `do.go`'s `doErrand`).

### 2.3 The root starts on the worker seat

The role is read off the store's shape at every launch:
`roleOf` (`internal/plandb/store.go:384`) answers `plan` only for a Composite
task, and a task becomes Composite only when a child is added under it
(`store.go:260`, inside `AddMany`). A fresh root has no children, so
`CrewFactory` (`crew.go:117`) seats its first turn on the **worker** model. The
decision whether and how to split is made by the cheapest seat in the crew.
The comment on `SeatFor` says the root rides the plan seat; that is true only
from its second launch on, once it has children. The existing test that pins
the plan seat for the root (`crew_test.go`,
`TestCrewFactoryRunsTheDoorsSeatsWhateverTheProfileSays`) adds children first.

The experiment switch on this branch, `CODEAF_EXPERIMENT_ROOT_PLAN_SEAT`
(`crew.go`, `RootPlanSeatEnv`), seats a childless root on the plan seat from
its first turn. It is off by default, registered as operator plumbing in
`internal/config/settings.go`, and pinned by
`TestRootPlanSeatSwitchSeatsOnlyTheChildlessRoot`.

### 2.4 There is no separate plan phase

The supervisor's first launch is the root (`internal/run/run.go:405-410`). The
root worker's brief is the ask itself (`BeltWorkerBrief`,
`internal/session/bashbelt_worker.go:162`), and the worker decides for itself
whether to do the work or to split it with `plandb add` / `plandb split`
(`internal/session/prompts/bashworker.md`, "The plan"). Every leaf later gets
the root's ask verbatim beside its own work order (`askSection`, same file).

### 2.5 Dispatch streams

Every pass (`run.go:343`, every 300 ms or on a return) launches the whole
`ReadySet().Runnable` (`run.go:412`). With `task.parallel` at its default of 0
there is no slot cap (`NewSupervisor`, `run.go:197`; `full`, `run.go:539`), so
a dependent starts the moment its dependency lands.

### 2.6 The review round

Every work-seat leaf that lands done, including a childless root, gets one
`check` task (`addReviewCheck`, `run.go:835`). A check answering
`does not hold:` leaves a note on the leaf and adds one `fix:` task under the
leaf's parent (`recordCheckFinding`, `run.go:897`). There is one round only: a
finding on a fix is a note and nothing more (`run.go:895`). A check's landing
wakes nobody (`needsWake`, `run.go:1294`). There is no road from a finding to
a change of plan: a finding becomes a fix, never a replan.

A check's `holds:` passes a gate in the store before it is accepted
(`Store.Done`, `internal/plandb/store.go:581`; `checkVerdictBasis`,
`store.go:661`): every command declared with `--check` must appear in the
check's own trajectory as a run on its own with a zero exit, and must be one
auditable command with no shell composition (`auditableDeclaredCheck`,
`store.go:785`). `plandb add --check` accepts any command at all
(`internal/plandb/cli.go:418`). So a planner can declare a check that no
checker can ever satisfy, and a checker that runs the declared command inside
a longer one line (`go test ./x && grep ...`) is refused too. Section 8 shows
what that costs.

### 2.7 Wakes

- **Integration wake.** A composite whose children have all landed
  (`childrenAllLanded`, `run.go:1349`) and include one it has not been told
  about (`needsWake`, `run.go:1268`) is launched again (`launchWakes`,
  `run.go:1026`) with the wake clause (`run.go:1466`): "every child you
  dispatched has landed. Integrate their results, verify the combined result
  in the workspace, add more children if something is missing, and report."
  A parent is woken at most `maxWakes` = 4 times (`run.go:62`), then closed
  (`closeAtCap`, `run.go:1415`).
- **Parked wait.** A worker that ran `plandb wait` is relaunched when nothing
  it waits on is open, or early when a child or dependency ended failed or
  cancelled (`launchWaits`, `run.go:1093`; `waitMoved`, `run.go:1194`).

So a parent hears about its children at the end, all at once, and mid-run only
if it parked itself and something failed.

### 2.8 What the engine records

Three records carry everything the baseline reads, and none of them needs a
model to interpret:

- the plan store: every task, its role, status and result, and one spend row
  per worker launch with its model and role (`recordSpend`,
  `internal/run/bashworker.go:768`, into `plandb` `AddSpend`,
  `store.go:2769`);
- a trajectory per task beside the store, one line per command with its exit
  code, and one opening line per launch (`bashworker.go:96`);
- the home's model-call log, one priced row per provider call
  (`internal/calllog`).

## 3. The proposal

- **(a) Integration modes, declared at split time:** `mechanical` (the parent
  only concatenates), `verify` (run a command over the combined result), or
  `synthesize` (a model reads every child and writes the answer).
- **(b) Split-time assumptions for each child:** what the planner believed when
  it cut the work ("the four packages share no code").
- **(c) Replanning triggers:** checkpoints the planner declares (a cheap probe
  first, then a fan-out sized by what it found), and surprise signals.
- **(d) The lowest affected parent wakes,** not the root.
- **(e) A replan edits only work that has not started.**
- **(f) A budget, batching, and learning across runs.**

## 4. The critique, and what it changes

| # | Critique | What the design does instead |
| --- | --- | --- |
| 1 | A worker's own "assumption held" is unreliable. | Triggers read hard signals only: a failed task, a check's `does not hold:`, the store's real spend, a red test run in a trajectory, two tasks editing one file. Assumptions stay as text a waking planner reads, never as a trigger. |
| 2 | A model's cost estimate is noise. | Budgets use the store's own spend rows, per role and model, across runs (`plandb` `SpendSummary` already groups them). |
| 3 | "Affects siblings" needs the sibling list in every brief, which grows O(N²). | A run-level findings board in `plandb`, written by hard signals and read only by a planner that wakes. Leaves never read it. (Leaves already carry the whole ask, which is O(N) and fine.) |
| 4 | Surprises cross subtrees. | The same board, keyed by file and by task, so a finding in one subtree is visible to the parent of another. |
| 5 | A mid-run wake races siblings that keep running. | Pause only the siblings a finding names (the store's `Pause` exists), never the whole level. |
| 6 | A merge or cancel can drop coverage silently. | A replan that cancels or merges tasks carries every original acceptance forward, and the store refuses an edit that would leave one uncovered. |
| 7 | Planners will over-declare checkpoints. | Each declared checkpoint is charged to the replan budget when it fires, like any other wake. |
| 8 | "Mechanical" integration can be abused. | Default to `verify` with the repository's own test command whenever a leaf changed code; `mechanical` only where no leaf touched code. |
| 9 | A value-of-information formula is theatre. | A trigger fires on a hard signal AND enough unstarted work to matter (for example two or more unstarted tasks under the affected parent). No expected-value arithmetic. |
| 10 | No evidence yet that replanning helps. | This baseline. Build only what the data below shows missing. |

## 5. The integration wake: what it is for, and when it is waste

**What it is for.** The woken-parent law (`run.go:1013-1025`) exists so a
parent's result is written after its children land, not before; otherwise the
run's answer is the planner's first sentence, written before any work was
done. The wake is also the one place, today, where the combined result is
looked at as a whole: every check reads one leaf against its own acceptance,
so a break between two leaves (one leaf's change turning another package red)
is only seen when a parent runs the whole suite over the shared working copy.

**When it is waste.**

- When every child already proved the whole acceptance, the whole suite
  included, and nothing a child did can break another. The wake then re-runs
  the suite and restates the children's results: a planner-seat call that
  changes nothing.
- When the only integration needed is mechanical (the children wrote separate
  files and nobody reads them together).
- When a single child was split off. The parent's wake is a second pass over
  one piece of work, on the most expensive seat.

**The cheaper shape** (critique 8): after the last child lands, run the
repository's test command over the working copy mechanically. Green and no
`synthesize` mode declared: close the parent with its children's results, no
model call. Red: wake the parent, with the failing output in its clause. The
model wake is then paid only when the combined result is actually broken.
Section 8 measures how often the wake did anything a mechanical run would not
have.

## 6. Which triggers to build first, pending the data

The table was written before the run; the last column was filled in from
section 8.

| Trigger | Signal | Hard? | What in the baseline would justify it | Decision after the baseline |
| --- | --- | --- | --- | --- |
| T1 Finding to replan | a check's `does not hold:`, or a failed leaf, with unstarted siblings | yes | checks that do not hold, fixes that repeat a sibling's cause | **wait**: 0 findings in 33 checks |
| T2 Overlap | two tasks edit the same file | yes | files edited by two tasks, r1's `canon.go` edited by more than one | **wait**: no shared source file in any run |
| T3 Integration verify | the test command red after all children land | yes | wakes that found a break, r3's cross-package failure | **build first**: r3's one wrong answer, and a wake that only re-ran the suite |
| T4 Declared checkpoint | a probe task lands, then the planner sizes the fan-out | planner-declared | r2's width reached versus the six call sites | **wait**: the root probes inside its own turn and fanned out full width at once |
| T5 Spend overrun | a task's spend passes its role's historical cost | yes | spend by role, the check's share of the bill | **build first**: checks at 40 times their leaf's cost, 74% of the grid |
| T6 Assumption broken | a worker says a split-time assumption failed | no (self-report) | none: excluded by critique 1 | not built |
| Seat | the root decides the split on the plan seat | n/a | B-rootplan against B-now on pass, cost and shape | **keep the switch, off**: faster, one more pass, twice the cost on small cells; n=2 |

And one that is not a trigger at all, and comes before all of them: make a
declared check one the gate can accept (section 9.1).

## 7. The measurement plan

### 7.1 Cells

Four Go fixtures under `bench/replan/fixtures/`, each hiding one structure.
Every cell is graded by code: the fixture's own `go test ./...` must pass and
every test file must still hold its seed bytes (`bench/replan/grade.go`). No
model judges anything.

| Cell | The brief says | What it hides | Graded by | Measured |
| --- | --- | --- | --- | --- |
| r1-shared-cause | seven separate bug reports | reports 1-5 are one bug in `canonical()` (`canon.go`); 6 and 7 are independent | suite green, tests untouched | tasks that edited `canon.go`; symptom files patched instead of the cause |
| r2-probe-then-fanout | move every caller off the deprecated `legacy` package, then delete it | six independent callers, each with its own retry count and 404 reading; none named | suite green, tests untouched, `legacy/` gone, no importer left | width reached, wall, tasks |
| r3-wrong-first-approach | change `money.Format` to the accountants' style | `export.CSV` formats through `money.Format` and its test wants the plain form, so changing Format alone turns `export` red | suite green, tests untouched | red test runs, edits to `money.go`, whether `export.go` was fixed |
| r4-control | the suite is failing, fix it | nothing: one function | suite green, tests untouched | overhead: tasks, checks, cost |

Each fixture was proved solvable with a reference fix, and r3's trap proved
red (Format changed, export untouched: `TestCSVIsWhatTheBankImports` fails).

### 7.2 Arms

- **B-now:** the bash belt as it is: `CODEAF_TASK_BELT=bash`,
  `CODEAF_EXPERIMENT_ROOT_PLAN_SEAT=off`.
- **B-rootplan:** the same, with the switch on, so the root's first turn rides
  the plan seat.

Both arms run `codeaf do -model deepseek/deepseek-v4-flash -plan-model
z-ai/glm-5.3`: one open-weight model per seat, the same on both arms. The plan
seat is the stronger open-weight model, so moving the root onto it changes the
model that makes the first split decision. The check seat follows the plan
seat. The throwaway profile pins every other tier to the same two models and
carries no key. The two arms differ in the one switch and nothing else
(`bench/replan/driver_test.go`, `TestArmsDifferOnlyInTheSwitch`).

### 7.3 What is read

Per invocation (`bench/replan/readings.go`): pass or fail; dollars from the
call log (every priced provider call), cross-checked against the store's spend
rows and the door's own envelope; wall; steps (trajectory command lines);
tasks by role (leaves, checks, fixes, probes, splits); check verdicts; root
launches and what the launches after the first cost; peak and mean workers at
once (the store polled every two seconds); test runs and red test runs; files
edited by two or more tasks; and each cell's own measure.

"Edited" is read from the commands the workers ran (`editedFiles`): the belt's
`codeaf patch`, redirections, `tee`, in-place `sed` and `perl`, `gofmt -w`,
python's `open(..., 'w')`, and `rm`. It is a heuristic over command text, and
its test (`TestEditedFilesReadsTheBeltsEditHands`) pins the shapes it knows.

### 7.4 How to run it

On a benchmark machine, in a fresh clone:

```sh
make build
go run ./bench/replan -dry-run                     # every invocation, executes nothing
go run ./bench/replan -replicates 2 -parallel 2 \
  -cell-cap 1.50 -total-cap 9.00 -timeout 20m      # the baseline grid
go run ./bench/replan -cells r3 -arms B-now -replicates 1   # one cell
```

The provider key is read from the driver's own environment and is never
written anywhere. The driver interrupts only the process it started, by its
recorded pid, when an invocation passes `-cell-cap` or the run passes
`-total-cap`; nothing new starts past the total cap. Rows land in
`replan.csv`, a per-invocation `row.json`, and `summary.md` under the output
root. `go run ./bench/replan -reread <output root>` reads a finished run again
from its records alone, with no model call, so a reader that improves can be
applied to rows already paid for (the table below was produced that way, after
a fix to how a woken task's launches are counted).

### 7.5 Budget

The brief's hard cap was $10 for the whole baseline and $5 for any single
cell. The driver ran with an interrupt at $1.50 per invocation and a stop at
$9.00 overall.

## 8. Baseline results

Run on 2026-09-23 at `53cd48212` (this branch), 16 invocations, two at a time,
on a shared benchmark machine. Reread at `4301e0739`. Dollars are the
call log's priced rows; the store's spend rows and the door's envelope agree
with them to the cent on every row except the one the driver interrupted.

**Spend: $2.13 for the grid, plus $0.02 for one smoke run before it. $2.16 in
all, against a $10 cap.** No cell reached $5; one invocation reached the
driver's $1.50 interrupt.

### 8.1 Every invocation

| cell | arm | rep | pass | ending | $ | wall s | steps | leaves | checks: held / refused | check $ | root wake $ | peak workers | cell measure |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| r1 | B-now | 1 | yes | done | 0.0166 | 51 | 14 | 0 | 1 / 0 | 0.0105 | | 1 | canon.go edited by 1 task, 0 symptom files patched |
| r1 | B-now | 2 | yes | done | 0.0166 | 47 | 12 | 0 | 1 / 0 | 0.0122 | | 1 | canon.go by 1, 0 patched |
| r1 | B-rootplan | 1 | yes | done | 0.0330 | 9 | 7 | 0 | 1 / 0 | 0.0088 | | 1 | canon.go by 1, 0 patched |
| r1 | B-rootplan | 2 | yes | done | 0.0284 | 14 | 8 | 0 | 1 / 0 | 0.0131 | | 1 | canon.go by 1, 0 patched |
| r2 | B-now | 1 | yes | done | 0.0946 | 142 | 100 | 7 | 7 / 0 | 0.0530 | 0.0064 | 6 | 6 callers, each edited by one task |
| r2 | B-now | 2 | yes | done | 0.2104 | 708 | 114 | 6 | 6 / 6 | 0.1697 | 0.0113 | 6 | 6 callers, each by one task |
| r2 | B-rootplan | 1 | yes | stopped at the $1.50 interrupt | 1.5003 | 708 | 280 | 7 | 0 / 7 | 1.4073 | | 7 | 6 callers, each by one task |
| r2 | B-rootplan | 2 | yes | done | 0.0478 | 15 | 10 | 0 | 1 / 0 | 0.0138 | | 1 | root did all six itself |
| r3 | B-now | 1 | **no** | done | 0.0294 | 92 | 17 | 0 | 1 / 0 | 0.0205 | | 1 | export.go untouched; suite red |
| r3 | B-now | 2 | yes | done | 0.0165 | 69 | 16 | 0 | 1 / 0 | 0.0097 | | 1 | export.go fixed |
| r3 | B-rootplan | 1 | yes | done | 0.0476 | 29 | 16 | 0 | 1 / 0 | 0.0112 | | 1 | export.go fixed; money.go edited in 4 steps |
| r3 | B-rootplan | 2 | yes | done | 0.0375 | 16 | 13 | 0 | 1 / 0 | 0.0076 | | 1 | export.go fixed; money.go in 3 steps |
| r4 | B-now | 1 | yes | done | 0.0105 | 24 | 11 | 0 | 1 / 0 | 0.0077 | | 1 | one edit |
| r4 | B-now | 2 | yes | done | 0.0079 | 27 | 11 | 0 | 1 / 0 | 0.0046 | | 1 | one edit |
| r4 | B-rootplan | 1 | yes | done | 0.0172 | 10 | 9 | 0 | 1 / 0 | 0.0062 | | 1 | one edit |
| r4 | B-rootplan | 2 | yes | done | 0.0194 | 6 | 9 | 0 | 1 / 0 | 0.0088 | | 1 | one edit |

"Leaves" is the work tasks the root split off (0 means the root did the work
itself). "Refused" counts checks whose `holds:` the store's gate turned down
at least once. "Peak workers" is the most leaf tasks running at once, checks
included. The raw rows, with every column the driver reads, are in each
invocation's `row.json` and in `replan-reread.csv`.

### 8.2 By arm

| arm | pass (by code) | total $ | median $ | median wall s | runs that split |
| --- | --- | --- | --- | --- | --- |
| B-now | 7 of 8 | 0.40 | 0.017 | 60 | 2 of 8 (both r2) |
| B-rootplan | 8 of 8 | 1.73 (0.23 without the interrupted run) | 0.035 | 15 | 1 of 8 (r2) |

### 8.3 What the numbers say

1. **The review round is most of the bill, and it never found anything.**
   Checks cost $1.76 of the grid's $2.13 (83%). Of 33 checks, none answered
   `does not hold:`, so the fix road and every replan trigger that would hang
   off a finding had nothing to fire on. Even with the gate trouble below set
   aside, the check cost more than the work it checked in every B-now run
   (56% to 81% of each run's bill), because the check rides the plan seat's
   model and the work rides the cheap one.

2. **Most of that money went to checks the store would not let answer.**
   $1.58 (74% of the whole grid) was spent in the 13 checks whose `holds:` the
   gate refused. Two shapes caused it, both in r2:
   - B-rootplan r2-1: the planner (the root, on the plan model) declared
     checks as one compound line (`cd <copy> && go test ./billing/ && ! grep -q
     legacy billing/billing.go && git diff --quiet -- ...`). `plandb add`
     accepted them; the gate refuses composed commands. All seven checks were
     refused, then spent 21 to 34 steps each working out why, reading the
     plan tool's source and running `strings` over its binary, at $0.16 to
     $0.26 each (the work they checked cost about $0.005 each) until the
     driver's interrupt.
   - B-now r2-2: the declared checks were single commands, but every checker
     ran them inside a longer line (`go test ./reviews/ && grep ...`), which
     the gate does not count as a run of the declared command. All six were
     refused once. Five recovered in a few steps. One went looking for the
     rule with `find /` over the whole machine, waited ten minutes on it, and
     then ran `pkill find` on a shared machine. That one check is the whole
     of that run's 708 seconds.

3. **The one wrong answer was a run that said done over a red suite.**
   B-now r3-1: the worker changed `money.Format`, saw `go test ./...` fail in
   `export`, reported "go test ./money passed" and finished. The check ran the
   whole suite, saw `export` fail, wrote a note saying so, and answered
   `holds:` anyway, reasoning that the tests could not be changed. The run
   ended done. A mechanical run of the repository's test command before the
   run answers would have caught it with no model call. Both B-rootplan runs
   and the other B-now run fixed `export` the first time.

4. **The hidden structures mostly did not bite, because the work did not
   fan out.** In r1 no run split the seven reports: one worker read the code,
   fixed `canonical()` once, and patched no symptom file, on both seats. In
   r3 no run split either. Only r2 split, and only in three of four runs.
   There was no duplicated edit anywhere: every caller in r2 was edited by
   exactly one task, and no two tasks edited the same source file.

5. **Fanning out did not buy wall time at this size.** r2's single-worker run
   (B-rootplan r2-2) finished in 15 seconds for $0.048. The fanned-out runs
   took 142, 708 and 708 seconds, and their six leaves each finished within
   about 30 seconds of the split; the rest of the wall was checks. The root also read every
   caller itself before splitting (16 steps), so the probe the proposal's
   checkpoints would add already happens inside the root's first turn, and
   every leaf then read its file again.

6. **The integration wake fired twice (both B-now r2) and cost 5% to 7% of
   its run.** In r2-1 it ran the suite and a grep and reported: nothing a
   mechanical test run would not have done. In r2-2 it did real work: the
   root had kept the deletion of `legacy/` for its own wake instead of making
   it a leaf.

7. **Seating the root on the plan model changed the shape more than the
   outcome.** B-rootplan passed 8 of 8 against 7 of 8, finished faster (median
   15 s against 60 s), split less, and cost about twice as much on the small
   cells, where its first turn did the whole job on the dearer model. Its one
   split wrote the uncheckable compound checks. Two replicates cannot separate
   these from noise.

### 8.4 Limits of this baseline

- Two replicates per cell and arm, two models, one day. Pass rates of 7 of 8
  and 8 of 8 are not a difference.
- The cells are small. Every leaf in r2 was about 30 seconds of work, so the
  fixed costs (a check per leaf, a wake) weigh more than they would on real
  tasks, and the benefit of width is as small as it will ever be.
- "Peak workers" is polled every two seconds and counts checks as workers.
- "Edited" is read from command text (`editedFiles`), a heuristic.
- Both arms share the check seat's model, so the check findings speak to the
  engine, not to the arm.

## 9. Conclusion

**Where the engine actually wastes or misses, on this evidence:**

1. The review round's gate and the planner's `--check` disagree, and a check
   that cannot satisfy the gate burns the most expensive seat until a wall.
   This was 74% of the whole baseline's spend, and 708 seconds of one run's
   wall.
2. The review round costs more than the work on small tasks and, in 33
   checks, never produced a finding, while it passed the one run that ended
   with a red suite.
3. Nothing checks the combined result mechanically before a run answers
   done.

**Where it does not waste, on this evidence:** duplicated work between leaves
(none seen), and fan-out width (the root fanned out to the full width at once
when it split at all).

### 9.1 What the data supports building first

None of these is a replanning layer; all three are cheap and measurable with
this bench.

1. **Make a declared check satisfiable, or refuse it at the door.** Either
   `plandb add --check` refuses what the gate would refuse (one command, no
   composition), naming why, or the gate accepts a declared command run as the
   first command of a line. And a check whose `holds:` has been refused twice
   ends with `check:` and its one sentence rather than investigating. Expected
   effect on this grid: most of the $1.58.
2. **A mechanical verify before done (T3).** When the last work task lands,
   the run runs the repository's test command over the working copy (for a
   Go module, `go test ./...`; in general, every check the run's tasks
   declared, run together over the combined result). Red: wake the lowest parent with the failing output in
   its clause, or the root when there is no split. Green: close without a
   model wake when no `synthesize` integration was declared. This catches r3's
   wrong answer and replaces the integration wake that only re-ran the suite.
3. **A spend-overrun trigger per role (T5).** A task whose spend passes a
   multiple of its role's historical cost (the store's spend rows already
   carry role and model) is stopped and reported. A check at forty times the
   cost of the leaf it reads is the plainest hard signal in this data.

Also worth doing, and not about replanning: a check should not ride the plan
seat by default when the work rides a cheaper one; on these cells it doubled
the bill and caught nothing.

### 9.2 What the data does not support yet

- **T1 (finding to replan):** zero findings in 33 checks. Nothing to trigger
  on until checks find things.
- **T2 (overlap) and the findings board (critique 3 and 4):** no two tasks
  edited the same source file in any run.
- **T4 (declared checkpoints, probe then fan-out):** the root already probes
  inside its own first turn, and fan-out was immediate and full width.
- **Integration modes (proposal a):** only as the `verify` default in 9.1.2;
  nothing here needed `synthesize`.
- **Seating the root on the plan seat by default:** the switch stays, off.
  It was faster and passed one more run, and it cost about twice as much on
  small cells; it needs larger cells and more replicates before it moves a
  default.

### 9.3 What surprised

- No run split r1's seven "separate" reports. Both seats read the code and
  fixed the one shared function once. The trap the cell was built around
  never sprang.
- A single worker beat the fan-out on r2's wall by a factor of ten, because
  the checks, not the leaves, set the finish line.
- A check worker, stuck on the gate, ran `find /` and then `pkill find` on a
  shared machine. The belt put no bound on either.
- `bench/bashloop`'s task door never reached this engine (section 2.1): its
  bash-belt numbers describe the legacy node road.

### 9.4 Next measurements

After 9.1.1 and 9.1.2 land, rerun this grid unchanged (same cells, same arms,
n=2, about $2) to see the check share fall and r3's wrong answer caught. Then
add two larger cells, with leaves of several minutes each, before deciding on
T1, T2 or T4: those triggers can only pay where the work is big enough for a
mid-run change of plan to save more than it costs.

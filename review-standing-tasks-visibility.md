# Review: standing-run task visibility — 5 claims checked against the code

Codebase: `/Users/santoshkumar/Documents/agentfield/code/codeaf`, commit `aae00e7a7`.
All citations are `file:line`. Each claim is confirmed or disproved against the
code, not the host task's reasoning.

---

## Claim 1 — tool registration and search path — **confirmed**

- `func (a *Agent) tasksTool() bare.Tool` at `tools_tasks.go:181`, with
  `Description: tasksDescription` (`tools_tasks.go:184`, const at `:72`).
  The no-id branch calls `a.taskSearchText(parsed.Query, parsed.Limit, scope)`
  (`tools_tasks.go:216`); the id branch calls `a.oneTask(ctx, token, parsed)`
  (`tools_tasks.go:223`).
- Belt registration: `tools.go:181` — `tools = append(tools, a.tasksTool())`,
  gated by `if a.mayProposeTask()` (`tools.go:179`). `mayProposeTask` is
  `!c.InTask || c.mayFanOut()` (`task.go:534`), and `mayFanOut` is
  `c.InTask && c.tasker != nil && fansOutAt(c.taskDepth)` (`task.go:540`).
  So an InTask firing with a graph (an armed one) does get the tool; an InTask
  firing without one does not. The eye item's run transcript calls `tasks`
  repeatedly, so the tool was present for that firing.
- `func (a *Agent) taskSearchText(query string, limit int, scope string) string`
  at `tools_tasks.go:246`. Its first row of data is
  `taskRowsTextLimit(a.taskRows(), query, limit)` at `:255`.
- `func (a *Agent) taskRows() []TaskIndexEntry` at `tools_tasks.go:935`:
  `parent := a.config.taskID; if parent == 0 { return a.TaskIndex() }`. So the
  no-id search reads the project index **only when `taskID == 0`**; a node
  (`taskID != 0`) reads its own graph children instead.
- `func (a *Agent) TaskIndex() []TaskIndexEntry` at `task_index.go:571`:
  `rows := ReadTaskIndex(a.config.taskIndexFile())` at `:572`.
- `func (c Config) taskIndexFile() string` at `task_index.go:404`:
  `if dir := strings.TrimSpace(c.Place.Dir); dir != "" { ... return filepath.Join(bucket, taskIndexName) }`,
  where `bucket := filepath.Dir(dir)` at `:406`. `taskIndexName` is `"tasks.jsonl"`
  (`task_index.go:75`).
- `func ReadTaskIndex(path string) []TaskIndexEntry` at `task_index.go:456`:
  `if strings.TrimSpace(path) == "" { return nil }` and
  `file, err := os.Open(path); if err != nil { return nil }` — a missing file
  returns nil.

The chain `taskSearchText → taskRows → TaskIndex → ReadTaskIndex(taskIndexFile())`
is exactly as claimed. One nuance the claim omits: `taskRows()` only reaches
`TaskIndex()` when `taskID == 0`; a node with `taskID != 0` never reads the
index file at all (it reads graph children). This matters for claims 4 and 5.

---

## Claim 2 — wrong path for a standing run — **confirmed**

- `func standingRunConfig(parent Config, item standing.Item, runDir string) (Config, error)`
  at `standing_run.go:766`. Line `:773`:
  `place := Place{Dir: runDir, Workspace: item.Workspace}`, then `:778`:
  `cfg.Place = place`.
- `runDir` comes from the standing store. `Store.RunsDir(id)` at
  `standing.go:653` is `filepath.Join(s.ItemDir(id), "runs")` =
  `<root>/<id>/runs`. The next-run folder is
  `filepath.Join(runs, fmt.Sprintf("%04d", attempt))` (`store.go:288`), so a
  run directory is `<root>/<id>/runs/0001`. The root is `v3StandingRoot()` =
  `home.Join("v3", "standing")` (`chatv3_standing.go:59`), so a run directory
  is `~/.codeaf/v3/standing/<item-id>/runs/0001`.
- `taskIndexFile()` at `:404-414` does `bucket := filepath.Dir(runDir)` =
  `~/.codeaf/v3/standing/<item-id>/runs`, then
  `filepath.Join(bucket, "tasks.jsonl")` =
  `~/.codeaf/v3/standing/<item-id>/runs/tasks.jsonl`.
  That is NOT the project bucket. The project bucket is
  `~/.codeaf/v3/projects/<workspace-hash>/`, which holds the real
  `tasks.jsonl`.
- Empirical confirmation on this machine: `find ~/.codeaf/v3/standing -name tasks.jsonl`
  returns nothing; `~/.codeaf/v3/projects/-Users-santoshkumar-Documents-agentfield-codeaf/tasks.jsonl`
  exists and is 253 KB.
- `ReadTaskIndex` returns nil for the missing file (`task_index.go:456-460`),
  so `TaskIndex()` returns nil, and the no-id search prints
  "No tasks have run in this project yet." (`tools_tasks.go:1109`). The real
  run transcript confirms this exact string.

---

## Claim 3 — `tellsElsewhere()` blocks elsewhere and everywhere — **confirmed**

- `func (a *Agent) tellsElsewhere() bool` at `taskdelta.go:721`:
  `if a.config.InTask || a.config.taskID != 0 { return false }` at `:722`,
  then `return strings.TrimSpace(a.config.Place.Dir) != ""` at `:725`.
- `standingRunConfig` sets `cfg.InTask = true` at `standing_run.go:780`.
  (`standing_run.go:365` also sets `InTask = true`, but that is in
  `probeTool`'s throwaway belt-probe agent at `:346-365` — a different code
  path, not the firing session.)
- `taskSearchText` at `tools_tasks.go:256`:
  `if !a.tellsElsewhere() { return a.taskConversationHint(out) }` — returns
  before the `taskElsewhereText` call at `:260` and before the
  `scope != taskScopeEverywhere` check at `:268`. So `scope=everywhere` cannot
  reach `OtherProjects` either: the gate is before the scope branch.

Confirmed: both the elsewhere and everywhere readings are blocked for a
standing firing. The claim cites `InTask = true` as the reason; for an armed
firing `taskID != 0` (see claim 4) would also trigger the same `return false`,
so the gate holds either way.

---

## Claim 4 — id query message and `taskID == 0` — **disagrees**

The claim asserts `taskID == 0` for standing runs, that the message is
"No task '4' in this project...", and that "the person paraphrased." The code
and the **real run transcript** show the opposite on all three points.

**What the code does.** `standingRunConfig` at `:766-790` does not set
`cfg.taskID` — the claim's parenthetical is true for that function alone. But
`Run()` at `:555-556` calls `standingWideWork(cfg, item, brief)` immediately
after, and `standingWideWork` at `standing_run.go:940-941` sets:

```go
cfg.tasker = graph
cfg.taskID = id   // the root node's id, from graph.reserve() at task_run.go:1281
```

...when the firing is armed for wide work: `if !cfg.Divide || !enumeratesWidth(...)`
returns false at `:884`. `cfg.Divide` comes from `v3StandingPosture` at
`chatv3_standing.go:192` (`Divide: settings.Swarm`), and `enumeratesWidth`
(`task_divide.go:635`) calls `splitgate.WorthIt` on the brief text.

**What the real run did.** The eye item `8eac8a7f9f983bf0` fired once
(`runs/0001`). Its transcript shows the `tasks` calls and their results:

```
tasks {"query":"Skills:","limit":20}     →  No task matches "Skills:". ...
tasks {"limit":30}                        →  No tasks have run in this project yet.
tasks {"limit":30,"scope":"everywhere"}  →  No tasks have run in this project yet.
tasks {"id":"4"}  →  No task "4" among the pieces you handed out. Call tasks with no arguments to see them; ...
tasks {"id":"5"}  →  No task "5" among the pieces you handed out. ...
tasks {"id":"6"}  →  No task "6" among the pieces you handed out. ...
```

The id-query message is **"No task \"4\" among the pieces you handed out"** —
the `taskID != 0` branch at `tools_tasks.go:648-651`:

```go
if a.config.taskID != 0 {
    return fmt.Sprintf("No task %q among the pieces you handed out. ...", token), true, nil
}
return fmt.Sprintf("No task %q in this project. ...", token), true, nil   // :653, the == 0 branch
```

So `taskID` was nonzero for this firing: `standingWideWork` armed it, and the
"among the pieces" branch fired — not the "in this project" branch the claim
names. The person did not paraphrase; the code produced that message.

**Why `taskRows()` is empty.** The claim says "taskRows() returns empty →
taskByToken can't find them" and attributes the emptiness to `ReadTaskIndex`
finding nothing (the wrong path from claim 2). That is the wrong mechanism for
this firing. With `taskID != 0`, `taskRows()` at `:935-948` never calls
`TaskIndex()`:

```go
func (a *Agent) taskRows() []TaskIndexEntry {
    parent := a.config.taskID
    if parent == 0 { return a.TaskIndex() }   // not taken
    // ...children of the root node from the graph...
}
```

It returns the root node's children from the graph. The eye did not divide, so
the root has no children, and `taskRows()` returns an empty slice. The wrong
index path (claim 2) is real, but it is not what produces the empty rows for an
armed firing — `TaskIndex()` is never reached.

**Summary of disagreement.** The claim is right that the id query fails and
that `taskRows()` is empty, but wrong about (a) which branch fires
(`taskID != 0`, not `== 0`), (b) the exact message ("among the pieces you
handed out", not "in this project"), (c) the cause of the empty rows (graph
children, not the index file), and (d) the claim that
"standingRunConfig never sets taskID" misses `standingWideWork` at `:941`
which does.

---

## Claim 5 — this is a bug, and no override exists — **confirmed, with one gap in the claim's reasoning**

**The path is wrong.** `taskIndexFile()` at `task_index.go:404-414` derives
the index path from `filepath.Dir(Place.Dir)`. For a normal session,
`Place.Dir` is `~/.codeaf/v3/projects/<workspace-hash>/<session-id>/`, so
`Dir` gives the project bucket `~/.codeaf/v3/projects/<workspace-hash>/` —
correct. For a standing firing, `Place.Dir` is
`~/.codeaf/v3/standing/<item-id>/runs/0001`, so `Dir` gives
`~/.codeaf/v3/standing/<item-id>/runs/` — wrong: no project index lives there.
The `task_index.go` header at `:40-56` states the design intent:
"In the PROJECT BUCKET ... The scope is the project and not the conversation,"
and "the parent of the folder is the bucket." A run folder's parent is `runs/`,
not the project bucket, so the derivation assumption breaks.

**No override exists.** I searched `task_index.go` for `standing` and `InTask`:
the only hit is `closeInflightTaskIndexRows` at `:686`, which guards a write
path (`if a.config.InTask { return }`) and has nothing to do with path
resolution. `taskIndexFile()` has no special case for standing runs.

**The claim's reasoning has a gap.** The claim says "the design intent is
that InTask agents (including standing firings) should see their own children"
and identifies the wrong path as the cause. The wrong path is real, but it is
not the cause of what the eye experienced. For an **armed** firing
(`taskID != 0` via `standingWideWork`), `taskRows()` at `:935` returns graph
children and never calls `TaskIndex()`, so fixing `taskIndexFile()` alone
would not let the eye see the conversation's tasks 4/5/6 — it would still get
its own (empty) children. The wrong path affects only the `taskID == 0` path
(the no-id search), which prints "No tasks have run in this project yet."
because `TaskIndex()` returns nil. The id-query failure has a second, more
direct cause: the armed firing's `taskID` makes `taskRows()` return graph
children instead of the project index.

The claim's recommendation — "a shell probe against the project's real task
index file ... NOT the `tasks` tool inside the firing" — is a design
suggestion I was asked not to make or evaluate; I confirm only its factual
basis: there is no override, and the path the `tasks` tool resolves is not the
project bucket.

---

## One-line summary

Claims 1, 2, 3, and 5 are confirmed by the code. Claim 4 is disproved: the
firing was armed (`standingWideWork` at `standing_run.go:941` set `cfg.taskID`),
so the real message was "among the pieces you handed out" (the `taskID != 0`
branch at `tools_tasks.go:651`), not "in this project" (the `taskID == 0`
branch at `:653`), and the empty rows came from the graph having no children,
not from `ReadTaskIndex` finding nothing — though the wrong index path from
claim 2 is independently real.

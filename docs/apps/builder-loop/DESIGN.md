# Builder on the linear task loop

Status: design, 2026-10-13. Branch `apps/builder-loop` (bin and desk).
Owner order: the Builder builds an app through the linear task loop with bash
and exposes its tools the way plandb is exposed. Stability comes before cost.

## 1. Bottom line

- A bash-belt task worker (`internal/run.BashWorker`, the seat `codeaf do` uses)
  builds the app over many turns.
- It never writes a big JSON reply. It types small `codeaf app …` verbs. Each
  verb checks one piece at once and either commits it to a **draft journal** or
  prints the exact error.
- codeaf renders `plan.json`, `app.json`, `screens/*.json`, `runs/*.yaml` and
  `agent.md` from the journal. Spec, Check and runtime do not change.
- Each committed verb is one journal line. The Builder tails the journal,
  renders, writes the files to the app's draft, and publishes one event on
  `app/<id>/draft`. The plan card and the Building screen fill in line by line.
- When a run fails, a **rescue ladder** takes over: it goes from the loop to
  today's fix calls, then to today's one-call pipeline. The new road cannot do
  worse than today on success rate.
- The switch is `CODEAF_APPS_BUILDER_ROAD=loop|calls`. The default is
  `rescue` (calls first, the loop only when the calls write fails); `calls` and `loop` stay selectable.

## 2. What exists today (the parts reused)

| Part | Where | Reused as |
|---|---|---|
| Single-call Builder: plan → write → Check → 3 fix rounds → drop failing runs | `internal/apps/builder` | The rescue ladder (§9) and the `calls` road |
| plandb store and shim: worker types `plandb add`, runtime reads the SQLite store | `internal/session/plandb_plan.go` (`armShim`, `planBashPrefix`), `bashbelt.go` (`planCommand`) | The exposure. `armShim` already writes a `codeaf` shim that execs the running binary, and `planBashPrefix` exports `PATH` + `PLANDB_DB` on every command |
| Run worker seat | `internal/run/bashworker.go` (`NewBashWorker(store, workspace, model, standing, completer).Run(ctx, task)`) | Hosts the Builder's worker. It also gives the trajectory, resume clause, notes delivery (`deliverNotes`), spend row and step cap (`WithStepsPerTask`) |
| Spec + validate + Check | `internal/apps/spec`, `internal/apps/check` | The checks behind verbs and `done` |
| Golden CRM | `internal/apps/spec/testdata/crm`, `builder/prompt/example` | The verb script on the teaching page, and the round-trip test |

`internal/session` and `internal/run` do not import `internal/apps` (checked
with `go list -deps`), so `apps/builder → run` makes no import cycle.

## 3. Shape

```
apps.create ─► Builder (loop road)
                 │ seeds plandb store  <build>/plan.db  (task "plan", later task "build")
                 │ opens draft journal <build>/draft.jsonl
                 ▼
           run.BashWorker.Run(task)            ← deepseek/deepseek-v4.1-flash
                 │ bash: export PATH=<build>/bin:$PATH PLANDB_DB=<build>/plan.db; codeaf app field deal stage enum --values …
                 ▼
           codeaf app <verb>  (same binary, via the codeaf shim)
                 │ flock · fold · validate piece · append line · print "ok …" or the exact error
                 ▼
           draft.jsonl ──tail (fsnotify)──► Builder: fold → Render → Store.WriteFiles → Hub app/<id>/draft + app/<id>/build
                                                     ▲
           owner plan-card edit ──► draft.Append(author=owner) + plandb note to the live task
```

`<build>` is `<apps root>/.builds/<app id>/`. It is outside the app's git folder,
so scratch files never reach a version.

## 4. Draft store — `internal/apps/draft` (new, pure Go, no services)

The store copies the plandb rule: **the worker writes through a CLI into a store
codeaf owns, and codeaf reads that store**.

- **Journal.** `draft.jsonl` is append-only. One `Entry` per line:
  `{seq, at, author: builder|owner|codeaf, verb, args, piece: {kind, id}, body}`.
  The fold of all entries is the truth, and nothing else holds state.
- **Lock.** Every append takes `flock(<build>/draft.lock)`, then folds, checks
  and appends. Without this, the worker's CLI and the owner's edits (server
  process) could race on the same append.
- **Fold.** `Fold([]Entry) State` gives `State{Name, About, Plan{Data, Runs,
  Screens, Agent}, Records map[id]*spec.RecordType (ordered), Screens
  map[id]json.RawMessage, Runs map[id][]byte, Agent []byte, Connectors,
  Triggers, Commands, Budget, Phase: planning|planned|building|done}`.
- **Render.** `Render(State, Defaults) spec.Files` is one pure function, so a
  test can compare its output with golden files. Some parts are derived rather
  than typed: `capabilities.records` (rw for each record), `capabilities.ai`
  (runs with agent steps), `agent.file`/`agent.model`, `budget` (from the
  create sheet), `kit`, `version` and `space`. A part that no verb has made yet
  is left out, so a half draft renders to a half app.
- **Find the draft.** The CLI opens `dir($PLANDB_DB)/draft.jsonl`, or it walks
  up from the working directory. `planBashPrefix` already exports `PLANDB_DB`,
  so **`internal/session` gets no change**.
- **Registry.** Verbs are registered as `map[string]Verb{Usage, Parse(args,
  stdin) (Entry, error), Check(State, Entry) error}`. There is no switch. Each
  verb is one small file with its own table test.

### 4.1 Package API (the L1 ↔ L2 contract)

```go
package draft // internal/apps/draft — imports spec only, never svc/store/records

const (
    JournalFile = "draft.jsonl"
    LockFile    = "draft.lock"
    SocketFile  = "builder.sock"
    DirEnv      = "CODEAF_APP_DRAFT" // optional override; else dir($PLANDB_DB); else walk up from cwd
)

type Author string // "builder" | "owner" | "codeaf"
type Piece struct{ Kind, ID string } // Kind: name|plan.data|plan.run|plan.screen|plan.agent|record|field|screen|run|agent|connect|trigger|command|drop|phase

type Entry struct {
    Seq    int      `json:"seq"`
    At     string   `json:"at"`
    Author Author   `json:"author"`
    Verb   string   `json:"verb"`
    Args   []string `json:"args,omitempty"`
    Body   string   `json:"body,omitempty"`
    Piece  Piece    `json:"piece"`
    Line   string   `json:"line"` // owner-readable sentence for the card/log; never a verb or an error
}

type Store struct{ /* dir */ }
func Open(dir string) (*Store, error)                       // creates the journal if absent
func (s *Store) Dir() string
func (s *Store) Append(a Author, verb string, args []string, body string) (Entry, error) // lock → fold → registry Parse+Check → append; error text is the CLI's exact line
func (s *Store) Entries(since int) ([]Entry, error)          // seq > since
func (s *Store) State() (State, error)

type State struct { /* Name, About, Plan spec.Plan, ordered records/screens/runs, Agent, Connectors, Triggers, Commands, Phase */ }
func Fold([]Entry) State
type Defaults struct{ Space, AgentModel string; BudgetMonth float64; Kit int; AppID string }
func Render(State, Defaults) spec.Files                     // pure; omits parts not made yet; plan.json always

// Server verbs: the CLI dials SocketFile; the Builder serves it.
type Request struct{ Verb string `json:"verb"`; Args []string `json:"args"` }
type Reply   struct{ OK bool `json:"ok"`; Text string `json:"text"` }
type Handler func(context.Context, Request) Reply
func Serve(ctx context.Context, dir string, h Handler) (stop func(), err error) // listens on dir/SocketFile, 0600

func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int // `codeaf app <verb>`; exit 0 ok, 1 refused, 2 usage, 3 Builder not running
```

`done` on the CLI is a server verb. The Builder runs Check, and on pass it
appends `Append("codeaf", "phase", ["done"], "")` and replies ok. The CLI never
writes the done phase itself.

## 5. Verbs — `codeaf app <verb>`

Every verb answers on one line: `ok <what changed>`, or `error: <exact
problem> — <fix hint>` with exit 1. Each verb checks against the folded draft,
so a screen that names an unknown field fails at the time it is added, not at
the final Check. A body (screen JSON, run YAML, agent text) is read from stdin,
so the worker sends it with a heredoc. One piece per call.

| Phase | Verb | Checks |
|---|---|---|
| plan | `name "<Name>" --about "<one sentence>"` | non-empty, ≤ 40 chars |
| plan | `plan data <id> "<line>"` · `plan run <id> "<line>"` · `plan screen <id> "<line>"` · `plan agent "<line>"` · `plan drop <group> <id>` | lowercase id, unique per group, non-empty line |
| plan | `plan ready` | name, at least 1 data, at least 1 screen, agent line → Phase=planned, the plan card shows Write |
| build | `record <id> --label L --plural P --title <field>` | id is in plan data (or `--new`); title field must exist by `done` |
| build | `field <record> <name> <type> [--required --unique --index --label L --values a,b --to <record> --default v --currency USD]` | type is in the spec field types; `ref --to` names a record; enum has values |
| build | `screen <id> < screen.json` | screen schema, kit parts and props, every binding names a record and field in the draft |
| build | `run <id> < run.yaml` | run schema, record/field refs (`run_references.go`), params, gate+action rule |
| build | `agent < agent.md` | `spec.ParseAgent` |
| build | `connect <connector> --can a,b --approve b` · `trigger <id> --schedule "<cron>" --run <id>` · `command <id> --label L --for <record> --run <id> [--param k=v]` | names exist; cron parses |
| build | `drop <kind> <id>` | the piece exists |
| read | `show [plan\|app\|screen <id>\|run <id>\|agent]` | prints the rendered file. The worker reads state here, never from files |
| read | `schema <screen\|run\|field\|agent>` · `catalog` | prints the embedded schema or kit, trimmed to what a verb needs |
| server | `check [--json]` | full Check on the rendered files with sample rows |
| server | `try <run-id>` | dry run of one run on sample data |
| server | `done "<one line>"` | full Check. Refuses while it fails and prints the report, then Phase=done |

**Server verbs** (`check`, `try`, `done`) need services the CLI process does
not have (records, sample seed, runtime). They are synchronous calls to the
running Builder over a **per-build unix socket** `<build>/builder.sock`
(mode 0600, served by the Builder while the build is live). The request is
`{verb, args}` and the reply is `{ok, text}`. The handlers call the same
`Service` code that the gateway methods call. If no server is listening, the
CLI fails at once (`error: the Builder is not running`, exit 3). There is no
wait loop.

*Why not the apps gateway (manager's note 1):* the gateway is reachable from
a worker shell (loopback, `bridge.Path`). But it sits behind the appapi guard,
which accepts only the launch token, and that token opens every app and appapi
route (keys, factories, shutdown). Exporting it into a shell that runs model
text gives the model all of that. A scoped token would need a second token
kind in `internal/appapi/guard.go` and a per-connection method allowlist in
the gateway, because method `Allow`s do not all refuse agent callers. The
per-build socket gives the same synchronous answer with no secret in the
shell and no change to the guard or the gateway. The scope is the file
itself: one socket per build, and it is gone when the build ends.

**Exact paths in errors (manager's note 2).** `screen`, `run` and `agent`
take one whole document per call. Each document is checked alone against its
schema and against the draft's records and fields. Each error names the file
and the JSON pointer, for example
`error: screens/pipeline.json: /columns/2/field "stge" is not a field of deal (fields: name, company, stage, …)`.
Another piece that is broken never blocks or fails this one, because a piece
is checked against the State, never against the full render. Cross-piece
problems that only the full render shows wait for `check` and `done`.

## 6. Exposure — copied from plandb

1. `codeaf app` grows the verbs beside `serve` in `cmd/codeaf/app_serve.go`
   (`runApp` dispatches `serve` or `draft.Main(args, stdin, stdout, stderr)`).
   This mirrors how `codeaf plandb` reaches `plandb.Main`.
2. The worker is a `run.BashWorker`, so `session.NewBeltWorker` arms the shim
   and the prefix by itself. `codeaf` in a worker's shell is always the running
   binary (`SetRunningCLI`), and `PLANDB_DB` names the build's store. Neither
   is new code.
3. The process environment never changes. The belt switch is the existing
   `CODEAF_TASK_BELT`. If it says `node`, the loop road is unavailable and the
   Builder takes the `calls` road and says so once in the log.
4. Lifecycle verbs stay with the runtime. The page tells the worker: never
   `plandb add` or `split`; finish with `codeaf app done`, then `plandb done`.

## 7. Builder side — `internal/apps/builder/loop*.go`

- **Road.** `road()` reads `CODEAF_APPS_BUILDER_ROAD` once in `New`. Registry:
  `map[string]roadImpl{"calls": callsRoad, "loop": loopRoad}`. `roadImpl` is
  `{plan(ctx,b) error; write(ctx,b) error; edit(ctx,b,edits) (Plan, error)}`.
  Today's `planThenMaybeWrite`, `writeAndCheck` and `applyEdits` become
  `callsRoad` without changing their bodies.
- **Seat.** One plandb store per app (`plandb.Open(<build>/plan.db, …)`), with
  two tasks in order:
  - `plan`: the paragraph, the create-sheet facts and the teaching page. It
    ends at `plan ready`, so the card shows.
  - `build`: added by Write. Its brief holds the current plan and the owner's
    edits.

  One `run.BashWorker` runs each task, with
  `WithStepsPerTask(plan 30, build 140)`, a wall of 4 min for plan and 15 min
  for build, and `WithSpendBank` charging `build.spent`. The ctx is cancelled at
  the budget cap (rule 10 holds). The completer is built from the same
  `clientConfig(model)` that `Model.Complete` uses: same provider stack, key
  ladder and billing receipts. The model is `deepseek/deepseek-v4.1-flash`
  (`DefaultModel`).
- **Tail.** A goroutine per live build watches the journal with fsnotify
  (already in go.mod). It falls back to a 250 ms size stat when the watch
  cannot be set, and always does one final read on worker exit. For each committed
  entry it folds, renders, runs `Store.WriteFiles` (coalesced to one write per
  tick), publishes a `DraftEvent` and a `BuildEvent` step line ("Data · Deal
  added"). It also serves `builder.sock` for the server verbs.
- **Restart.** `buildFor` rebuilds from the journal fold when one exists.
  Write after a restart starts the same task again. `BashWorker` already opens
  with the resume clause from the trajectory, and the draft is already there.
- **Cancel.** It cancels the worker ctx. The journal stays, and the app returns
  to planning, as today.

## 8. Events and owner steering

- New topic `bridge.TopicDraft(app) = "app/<id>/draft"`. Payload
  `DraftEvent{Seq, Kind: name|plan.data|plan.run|plan.screen|plan.agent|record|field|screen|run|agent|connect|trigger|command|drop|phase, ID, Line, Author, At}`.
  `Line` is the owner-readable sentence ("Deal · name, company, stage…"). It is
  never a verb or an error.
- Existing `app/<id>/build` events keep firing with the same shapes, so the
  Building screen works unchanged. It only gets more step lines, sooner. The
  final `plan`/`done` event still carries the whole plan, so an old desk still
  works.
- **Owner edits** (`builder.plan.edit`) on the loop road:
  - A line edit becomes `draft.Append(author=owner)`, with the event published
    at once.
  - When a worker is live, the edit is also added as a plandb note to its task
    ("Owner changed screen deals: …"). `BashWorker.deliverNotes` hands it to
    the worker on its next turn. This is plandb steering.
  - A free request in planning with no live worker starts a short `replan`
    task with the request as its brief.
  - Edits are also accepted while building (the brief allows this). The worker
    is told, and Check gates `done` as usual.

## 9. Stability: the rescue ladder

The rescue second pass is bounded at 60 steps, 6 minutes and $0.30 (live median 185 s; 5 of 6 under 240 s and $0.10); past a bound the ladder decides, and the failed card reads "The second pass could not finish".

The loop is first. No run that ends badly reaches the owner as a raw error.

1. Worker reached `codeaf app done`, so Check passed → seed sample → Preview
   (today's preview code).
2. Worker ended without `done` (step cap, wall, provider error, a stopped
   reply) but the draft renders to at least an app.json and one screen → today's
   `checkAndFix(files)`: up to 3 fix calls, then today's `fallback.go` drops the
   failing runs.
3. The draft is too thin → today's whole one-call pipeline from the plan
   (`writeAndCheck`).
4. All three fail → today's failed card (Fix it · Cancel), with owner words
   only.

Each rung is one log line in owner words. The rung used is recorded in
`result.json` (`road: loop|loop+fix|calls`) so the measurement can count it.

## 10. Teaching page — `internal/apps/builder/prompt/appbuilder.md`

It is embedded and goes in the task brief, so the worker does not spend a turn
reading it. It has:

- **Role.** You build one app through `codeaf app`. There are no files to write.
- **Order.** Name and purpose → plan lines → `plan ready` (plan task ends) /
  records and fields → screens → runs → agent → connect/trigger/command →
  `check` → `try` each run → `done`.
- **Verb table.** One line each.
- **Error rule.** A verb that answers `error:` changed nothing. Fix that one
  piece on the next call. Never retry the same text.
- **The golden CRM as a verb script.** A test runs the script through
  `draft.Main` on a temp journal and requires that `Render` matches
  `spec.GoldenCRM()` with JSON and YAML compared by meaning. The page cannot
  drift from the verbs.
- **Finish rule.** `codeaf app done` must print `ok`, then run `plandb done`.

The worker reads the page together with the standing `bashtask.md` loop page.
The page states that a Builder task has one owned output and never splits.

## 11. Measurement (acceptance)

`bench/appbuilder/` is an in-process driver of `builder.Service` with the real
model door. Matrix: 4 paragraphs (ux ledger + q-models) × 3 seeds × {calls,
loop}, all roles on OpenRouter `deepseek/deepseek-v4.1-flash`. Heavy runs go to
the Mac Studio while Spark load is above 40.

| Metric | Bar (loop road) |
|---|---|
| Check passes and the app opens in Use | ≥ 11/12 |
| Raw errors shown to owner (BuildEvent/DraftEvent text matching Go error shapes) | 0 |
| Every run passes `try` on sample data | 12/12 |
| Time to first draft line, total time, cost (billed receipts, same price table both roads), rescue rung used | reported next to calls |

The output is a CSV plus `bench/appbuilder/REPORT.md`. The default flips to
`loop` only after the bar holds. That is the owner's decision, with the report.

## 12. Tests

- `draft`: table tests per verb (accept, each refusal text), fold, lock under
  two concurrent writers, render vs golden, CRM script round trip, socket
  absent → exit 3.
- `builder`: loop road with a scripted completer. The worker types the CRM
  script → Preview. The worker stops halfway → rung 2. An empty draft → rung 3.
  Owner edit mid-build → note delivered. Restart → resume.
- Structural tests: `internal/apps/draft` imports no service package; the
  builder reads the road switch in one place; `TestBuilderNeverReadsRecords`
  still holds.
- Full `go test ./...`: no new failures against the base `b5578302b`.
- Desk: vitest for `useDraft` and the plan card folding streamed lines;
  Playwright `e2e/apps-builder.spec.ts` sees plan lines appear before
  `plan/done`.

## 13. Desk (branch apps/builder-loop in apps-desk)

1. `src/lib/types/apps.ts`: add `AppsDraftEvent`, `appsTopics.draft`, and
   `AppsTopicEvents.draft`.
2. `src/lib/apps/bridge/client.ts`: add the `app/${string}/draft` branch to
   `AppsEventFor`.
3. `src/lib/apps/bridge/hooks.ts`: add `useDraft(app)`, modelled on `useBuild`
   and de-duplicated by seq.
4. `src/features/app-create/connected.tsx`: `draftPlan(events)` folds the
   events into a partial `AppsPlan`. `planOf()` uses it while there is no
   plan.json, so `PlanCard` renders lines as they land.
5. `scripts/mock-apps.mjs` publishes draft events before `plan/done`.

A line that arrives while the owner edits does not re-render the line under the
cursor. Lines are keyed by id, and the edit field holds its own value.

## 14. Lanes

| Lane | Scope | Depends on |
|---|---|---|
| L1 draft | `internal/apps/draft` + `cmd/codeaf` dispatch + CRM script test | design |
| L2 loop road | `builder/loop*.go`, road registry, seat, tail, builder.sock, rescue ladder, events, steering | the L1 API in §4 (codes against it in parallel) |
| L3 page | `appbuilder.md` + CRM verb script (handed to L1's test) | the §5 verb syntax |
| L4 desk | §13 + vitest + Playwright | the §8 event shape |
| L5 bench | `bench/appbuilder` driver, both roads, report | L1+L2 merged on the branch |

Sonnet lanes in own worktrees off `apps/builder-loop`; codex exec for review
passes. No push to `apps/v1` before 21:30. Landing goes through general with
the full gate.

## 15. Out of scope tonight

- The app agent on the loop (`internal/apps/agent`). It is a later step on the
  same seat.
- Making `loop` the default. That waits for §11.
- A single-run executor behind `svc.Check` for `try`. Today `try <run>` runs
  the full Check and shows only that run's findings.

# Delegates — handing a task to an outside program — DESIGN (draft)

> **Superseded in part on 2026-09-23.** The owner moved the first release to
> programs BUILT INTO codeaf: no manifests, no `~/.codeaf/delegates`, no install,
> no `/delegate`; senior-dev copied into `internal/seniordev` from swe-pro-go at
> the tag `codeaf-absorb` (`6103488`); its CLI is `codeaf senior-dev`; every
> model call goes through a per-run model API codeaf serves; and the task page
> shows the program's conversation with codeaf (since 2026-09-24, the actions it
> took, step by step, with the conversation one key away). The protocol is now internal,
> version 2: [PROTOCOL.md](PROTOCOL.md). What follows is the v1 design as it was
> built; the manifest road is kept on the tag `delegate-manifest-v1`. The run
> road, the answer folded in for text, the stop and the reader below all carry
> over.
>
> **Superseded again on 2026-09-24: a program works in the folder itself.** The
> owner asked why it was so hard to have senior-dev just work on the problem,
> and the copy per run, the brief's paths rewritten to name it, the squash and
> the HEAD-restoring landing were all deleted. A program that edits files now
> works in the folder the task names, on a branch codeaf cuts for it there when
> the folder is a git repository, and when it ends codeaf commits what it left
> onto that branch and leaves it checked out; the person's branch never moves.
> The contract is the header of `internal/session/programfolder.go`, and
> [PROTOCOL.md](PROTOCOL.md) §2 says it. Every "working copy", "squash" and
> "merge home" below is the design as it was built before that day.


*2026-09-21, revised 2026-09-23. Written against `dev @ 17ae56d34` and
`swe-pro-go @ 6103488` (branch `zeropoint95/improvements`, PR #30). Waves 1 to
4 are built on this branch; every senior-dev change this asked for has landed.*

*The first delegate was called `swe-pro` when this was written. It was renamed
`senior-dev` in its own repository on 2026-09-22 (`b43daaf`): the binary,
`cmd/senior-dev`, the `.senior-dev/` run folder, `refs/senior-dev/*` and every
`SENIOR_DEV_*` variable. The repository and Go module keep the name
`swe-pro-go`. The commit pins below predate the rename and are still in its
history.*

## In one paragraph

A **delegate** is an outside program that does a whole coding task on its own.
You start one by typing its name as a command:

```
/senior-dev rewrite the auth middleware to use the new session store
```

That starts an ordinary **task**. It runs in the folder itself, on a branch of
its own in a repository, under your dollar and time limits, shows on the rail,
can be stopped, and leaves its branch checked out when it ends. The chat is not
blocked while it runs, but nothing else of codeaf's writes in that folder until
it ends (internal/session's programhold.go). Inside codeaf, a
delegate is one more **worker kind** behind the existing run supervisor. It is
not a second engine.

`senior-dev` is the first delegate. Others are added later, one manifest each,
at the person's discretion.

## Decisions already taken

| decision | answer | date |
| --- | --- | --- |
| Name | **delegate**, not sub-harness (that word is taken, see below) | 2026-09-21 |
| Command | `/<name> <brief>`, one word per installed delegate | 2026-09-21 |
| What it starts | a task through the existing `/task` door, never a blocking turn | 2026-09-21 |
| Questions from the delegate | none. The brief must be self-sufficient | 2026-09-21 |
| senior-dev's `wip(edit)` commits | squashed into one commit at landing (2026-09-21); kept on the program's own branch, under one commit of what it left uncommitted, from 2026-09-24 | 2026-09-24 |
| senior-dev control plane | optional. Landed in senior-dev `f3b9716` | 2026-09-21 |
| Live cost from senior-dev | a top-level `spend` record. Landed in senior-dev `5793499` | 2026-09-22 |
| Steps from senior-dev | a `step` record per finished tool call. Landed in senior-dev `5793499` | 2026-09-22 |
| Command rows and the manual law | rows are generated at launch; each delegate ships its own manual page; the law is checked at load | 2026-09-21 |
| Readers | **one generic reader**, compiled in, over a small stdout protocol. No per-program reader | 2026-09-21 |
| Delegates that produce no tree | allowed. The manifest says `"lands": "text"` and the terminal record's text is the deliverable | 2026-09-21 |
| Stage records on the task page | stages feed the live step only; `step` records are the trajectory, so the step count is what the program said it did | 2026-09-22 |
| The task page draws actions, not a dialogue | **superseded the row above, 2026-09-24.** Every stage, step and ending is also written to the task's action log (`delegate-actions.jsonl`) as it arrives, stamped with codeaf's clock; the page draws the program's actions under the steps of its own process through the program's own vocabulary (`Delegate.Present`), and the raw calls are one key away (`ctrl+y`). The live step names the step the program is in. Steps still feed the trajectory | 2026-09-24 |
| Review round on a delegated run | none. A check seat is a bash-belt worker the belt switch may have left off; the program's own checking is in its result | 2026-09-22 |
| The run road and the belt switch | a delegated run takes the run road whatever `CODEAF_TASK_BELT` says; only the worker kind differs | 2026-09-22 |
| A delegate runs alone | nothing joins a delegated run and no delegate joins a run underway; both are refused naming the busy folder | 2026-09-22 |

## Why not "sub-harness"

The word already means something else in this repository:

- `/harness` lists **saved shapes of work**: small programs built from this
  binary's own node kinds (agent loop, tool call, verify, human gate). A
  designer model builds one in conversation and saves it under
  `~/.codeaf/harnesses/<name>/vN.json`.
- `/subharness` lists those plus bundles on disk and built-ins, and opens an
  intake card for one.

Both run **inside this process**. A delegate is an **outside binary** codeaf
cannot see into. One word for both would confuse the manual, and the manual
is what the chat answers from.

| command | what it starts | who wrote it | where it runs |
| --- | --- | --- | --- |
| `/harness` | a saved shape of work | codeaf, at your request | in this process |
| `/subharness` | the same, through an intake card | codeaf or a bundle author | in this process |
| `/senior-dev` | an outside program | someone else | a child process in a working copy |

## The command

`/senior-dev <brief>` is `/task <brief>` with the worker already chosen.

1. The same card appears. You answer it before money moves.
2. The turn ends. You are not held for the hour.
3. A run starts in the tasks store, in its own working copy.
4. It shows on the rail with a live step. `stop` works.
5. When it ends, the landing wakes a turn, as every task does today. That
   turn reads senior-dev's terminal record and the landing note, and answers.

The model never watches the stream. You watch the rail.

**Rows are generated.** A manifest at `~/.codeaf/delegates/<name>.json` whose
binary is on PATH adds one row `/<name> <brief>` to the live command list, so
`/help` and the picker show it beside `/task`. No senior-dev on the machine means
no `/senior-dev` row. A name that collides with a built-in command or alias is
refused, naming the row.

**The model can propose one too.** `propose_task` gets an optional `via`
field. The system prompt names installed delegates the same conditional way
it names everything else (`HANDOFF_FACTS` in `beltfacts.go`), and says when
to pick one: a change big enough to want its own agent for an hour, specified
well enough that nobody will be asked anything.

**`/delegate`** (bare) lists the delegates on this machine: the binary each
resolved to, and the last run in two words. It answers "which do I have here".

## Where it plugs in

The run supervisor already gives every worker what a delegate needs. The
worker contract is one method:

```go
// internal/run/worker.go
type Worker interface {
    Run(ctx context.Context, task plandb.Task) (Report, error)
}
type Report struct { Result string; Steps int; USD float64; Waiting bool }
```

| already exists | where |
| --- | --- |
| cost and time ceilings handed to the worker | `run.Limits` |
| a spend bank the worker reports rising dollars into | `run.WithSpendBank` |
| the live step the rail draws | `plandb.Store.SetLive` / `ClearLive` |
| the trajectory the task page opens | `trajectory.jsonl` |
| a working copy cut per run | `task_run_copy.go` |
| landing and merge home | `run.Land`, `landBeltRun` |
| stop, proven reachable for every running row | `session.Cancel`, `stoplaw_test.go` |
| the run's dollars folded into the conversation total | `driveBeltRun`'s `foldSpend`, #1280 |

**A delegate is a second `run.Worker`.** `CrewFactory` picks a worker per task
by role today. It gains one branch: a task whose row names a delegate gets a
`delegate.Worker` instead of a `BashWorker`. Nothing above the factory changes.

**The floor.** With no change at all, the model can run `senior-dev run …`
through the `bash` tool in the background. That gives a job log and an exit
notice, and none of the rows in the table above. That gap is what this design
pays for.

## The contract a program must meet

| the program must | senior-dev today |
| --- | --- |
| **launch** from argv with brief, directory, dollar ceiling, wall ceiling | `senior-dev run --dir D --max-cost X --max-hours H -- "goal"` |
| **stream** progress as one JSON object per line on stdout, nothing else | yes, EVENTS-CONTRACT.md |
| **end** with exactly one terminal record: status, reason, `cost_usd` | yes, `{"type":"terminal",…}` |
| **stop** cleanly on SIGTERM, still writing the terminal record | yes. Only SIGKILL loses it |
| **leave its work in the tree** it was given, and nothing else | yes. `.senior-dev/` is git-excluded |

Two things codeaf does **not** ask, and the manual page says so:

- **No questions.** senior-dev auto-rejects its own `question` tool and has no
  stdin road. Write the brief so nobody needs to be asked.
- **No step cap.** senior-dev has cost and hours only. The step count on the
  task page is whatever the reader can count off the stream.

### The manifest

```jsonc
// ~/.codeaf/delegates/senior-dev.json
{
  "name": "senior-dev",                                 // also the command: /senior-dev <brief>
  "description": "an autonomous coding agent for one large, well-specified change",
  "bin": "senior-dev",                                  // resolved on PATH; a path is allowed
  "argv": ["run", "--dir", "{{workspace}}",
           "--max-cost", "{{cost_usd}}", "--max-hours", "{{hours}}",
           "--", "{{brief}}"],
  "env": { "OPENROUTER_API_KEY": "{{key:openrouter}}" },
  "lands": "tree",                                   // "tree": squash and merge the copy; "text": the terminal's text is the answer
  "limits": { "cost": true, "elapsed": true, "steps": false, "questions": false }
}
```

- `{{key:openrouter}}` resolves through `config.APIKeyAt`, the same door every
  lane uses, so a key pasted at first-run setup reaches the delegate (#576).
- A `bin` not on PATH means the delegate is absent, not broken. `/delegate`
  draws one dim line naming the binary it looked for.
- A `manual.md` ships beside the manifest. See *The manual law* below.

### The protocol, and the one reader

There is **one reader**, compiled in. It reads a small protocol on the
program's stdout: one JSON object per line, four record types, everything
else ignored. Ignoring the rest is what makes it generic: senior-dev's bus
payloads pass straight through it.

| record | required fields | the reader makes it |
| --- | --- | --- |
| `{"type":"stage","stage":S,"status":T}` | `stage`, `status` | the live step, `S · T`, and one trajectory line |
| `{"type":"spend","cost_usd":C}` | `cost_usd`, cumulative, non-decreasing | banked spend |
| `{"type":"step","command":X,"observation":Y}` | `command`; `observation` optional | one trajectory step. `Steps` counts these. Optional: a program with no steps is drawn by its stages |
| `{"type":"terminal","status":U,"message":M,"data":{"cost_usd":C,…}}` | `status`, `message`, `data.cost_usd` | the `Report` and the outcome. Exactly one, last |

`terminal.status` is a closed set, and it is senior-dev's:

| `status` | run outcome | rail word |
| --- | --- | --- |
| `pass` | done | done |
| `fail` | ran and did not finish | incomplete |
| `budget-exhausted` | a limit you set stopped it | stopped, naming the limit (#1279) |
| `crashed` | ran and did not finish | incomplete |

Process exit with no terminal seen is `ran and did not finish`, naming the
last stage seen. Optional `data` keys the landing note reads when present:
`reason`, `claim` (what the program's model said), `observed` (what the
program itself saw), `deliverable` (the answer text, for `"lands": "text"`).

**What this costs each program:**

- **senior-dev** emits all four in exactly this shape as of `5793499`. Its
  `step` is one per tool call reaching `completed` or `error`, never twice
  for a republished part; `command` is `tool: argument`, the argument capped
  at 200 bytes; `observation` is the output or the error string, capped at
  2048 bytes on a rune boundary. Nothing to adapt.
- **pr-af** needs a one-shot mode that prints these four records and exits:
  `stage` per review phase, `spend` per model call, `terminal` with the
  findings as `data.deliverable`, and `"lands": "text"` in its manifest.

**Never sum `cost` off senior-dev's `message.updated`.** An assistant message is
written more than once, so a naive sum double-counts. The `spend` record
exists for exactly this reason.

senior-dev keeps the model's claim and its own observation as separate fields.
The landing note keeps them separate too: *senior-dev says it submitted; its
verification failed 2 of 5 commands* is two sentences.

### Two kinds of landing

| `lands` | working copy | when the program ends |
| --- | --- | --- |
| `tree` (senior-dev) | cut per run, passed as `{{workspace}}` | squash, merge home, landing card |
| `text` (pr-af) | none; `{{workspace}}` is the person's folder, read-only by contract | `data.deliverable` is folded into the conversation the way a quick task's answer is, and the woken turn reads it |

### Money

1. senior-dev spends the person's key outside codeaf's provider ledger.
2. The reader hands every rising `spend` figure to the supervisor's bank.
3. At the ceiling the supervisor cancels the context, which sends SIGTERM,
   which lets senior-dev write its terminal record.
4. The conversation total, `/cost` and the status line move through
   `foldSpend`, as for any run.
5. The on-disk usage ledger does **not** get senior-dev's calls, because they
   did not go through a codeaf lane. The spending page says `via senior-dev`.

The ceiling passed on the command line is what is left of the smaller of the
conversation's limits (`runCostLeft`, #1281), so senior-dev cuts itself first.

### Stopping

`stop` on the row is `session.Cancel` with a new kind, `delegate`, listed in
`stoplaw_test.go` with its proving test. The worker terminates the process
group, waits the job grace, then kills. A terminal record inside the grace is
read and folded. Without one the row reads `stopped` with the last stage seen.

### Landing a `tree` delegate

*As built on 2026-09-21 and deleted on 2026-09-24: since then senior-dev works in
the person's folder on a branch of its own, its `wip(edit)` commits stay on that
branch, what it left uncommitted is committed there when it ends, and the branch
is left checked out rather than merged. `refs/senior-dev/*` are written into the
person's repository and overwritten by the next run.*

1. senior-dev works in the run's own copy, passed as `--dir`.
2. senior-dev commits every edit as it goes: `wip(edit): <path>`, dozens per run.
   These stay on inside the copy, because senior-dev's crash recovery and its
   restore-after-ship read them.
3. At landing codeaf **squashes** everything past the cut point into one
   commit. Subject: the task's title. Body: two sentences from the terminal
   record, what the model claimed and what senior-dev observed.
4. That one commit merges home the way every task lands.
5. `.senior-dev/` is git-excluded in the copy and never lands. `refs/senior-dev/*`
   die with the copy.

## The manual law

Delegates are added at a person's discretion, so `/<name>` rows cannot be a
build-time list, and a page compiled into every binary cannot explain them.
The law stays: every command the chat offers is explained in the corpus. Where
it is enforced moves.

1. **The static table keeps its static gate.** `internal/tui3`'s `commands`
   and its test are unchanged. Delegate rows are appended to the live list at
   launch and never enter the Go literal.
2. **Each delegate ships `manual.md`** beside its manifest, following the same
   rules as `internal/manual/chat/` pages. At launch the chat's corpus is the
   packed corpus plus an **overlay** of installed delegate pages. `manual.Corpus`
   gains one constructor that layers pages over another corpus. The `manual`
   tool then answers "what does /senior-dev do" from senior-dev's own page.
3. **The check runs at load.** A page that does not mention `/<name>` refuses
   the manifest. `/delegate` shows why: `senior-dev: its manual page does not say
   /senior-dev — not added`.
4. **One built-in page explains the family.** *Delegates — programs codeaf can
   hand a task to* mentions `/delegate` and answers "what is a delegate", "how
   do I add one", "why is there no /senior-dev here". It never names a delegate
   the build cannot promise exists.

## What senior-dev changed for this

Landed 2026-09-21 and 2026-09-22 on `zeropoint95/improvements`, PR #30.

1. **Control plane optional** (`f3b9716`). Reachable: mirrored as before.
   Unreachable: one stderr line, and the run proceeds. The `run-contract`
   record carries `"control_plane": {"enabled": false, "url": "<probed url>"}`.
   `senior-dev serve` still requires a plane. The manifest sets no `SENIOR_DEV_CP_*`
   variable.
2. **Live spend record** (`5793499`). `{"type":"spend","cost_usd":0.0213,"ts":…}`,
   top-level, one per completed assistant message, cumulative, compaction
   included. Emitted even at zero. Not projected onto the control plane.
3. **Step record** (`5793499`). `{"type":"step","command":"bash: go test ./...","observation":"…","ts":…}`,
   one per finished tool call. stdout only, not in the stderr trace.
4. **No question road**, by decision. Auto-reject stays.

Both stream additions were verified on the senior-dev side to touch only the
event layer: nothing under its engine, session, prompt builders or tool-result
path changed, and a standing test asserts the exact stdout record count.

Checked by the senior-dev side against its code: the outcome table above holds,
SIGTERM still writes the terminal record, and `--` before the goal parses.

## Waves

| # | lands | proof |
| --- | --- | --- |
| **1** ✓ | `internal/delegate`: manifest and loader; `Worker` (spawn under `processgroup`, stream to the reader, SIGTERM then kill, `Report`); the one generic reader and its protocol, already written down in `docs/DELEGATE-PROTOCOL.md` | unit tests against a fake binary emitting scripted protocol lines and honouring SIGTERM; the outcome table pinned; a recorded senior-dev stream replayed through the reader |
| **2** ✓ | the door (`via` rides the run, not a store column: a delegated run is one task); `CrewFactory` branches on it; generated `/<name>` rows and `/delegate`; `propose_task.via`; `HANDOFF_FACTS`; the `delegate` cancel kind; squash-then-merge landing for `tree`, text fold for `text`; `via` on the spend row | focused `internal/session` and `internal/tui3` tests |
| **3** ✓ | the manual: the built-in *Delegates* page; the corpus overlay; the load-time page check; senior-dev's own `manual.md` | `internal/manual/chat_test.go` probes: "can you hand this to senior-dev", "what does /senior-dev do", "why can't the delegate ask me", "difference between /harness and /senior-dev" |
| **4** ✓ | hosted: the door crosses the wire (`Delegate.List`, `Delegate.Start`, wire version 18), so a `--host` surface generates its rows from the far machine's registry and a delegate runs there | `internal/remote` surface-door law; `internal/tui3` delegate tests |
| later | `codeaf do` speaking the protocol so codeaf on another machine is a delegate; pr-af's one-shot mode; delegates chosen by crew seat; answering a delegate's question | — |

Wave 1 has no door and spends no money. Wave 2 is the first thing a person
can type.

## Open questions

1. **Who picks senior-dev's models.** Today its own `--high` default. The manifest
   could pass codeaf's work seat, but senior-dev speaks OpenRouter slugs and the
   seat may be on another lane. First cut: the manifest's argv, no seat.

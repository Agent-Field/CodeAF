# Delegates — handing a task to an outside harness — DESIGN (draft)

*2026-09-21, written against `dev @ 17ae56d34` and `swe-pro-go @ 4c3084f`
(branch `zeropoint95/improvements`). Status: a draft for discussion. Nothing
here is built.*

## The one sentence

A **delegate** is an outside program that can do a whole coding task on its
own; codeaf hands it a task the way it hands one to its own worker — in a
working copy of its own, under the conversation's dollar and time limits,
drawn on the rail while it runs, landed on the branch when it ends — and the
program is one more **worker kind** behind the run supervisor, not a new
engine and not a slash command of its own.

`swe-pro` is the first delegate. `codeaf do` on another machine is the second,
and it costs nothing extra, which is the test that the mechanism is general.

## Why not the name "sub-harness"

The word is taken, in code and in the manual. `internal/subharness`,
`/subharness`, `/harness`, `docs/SUBHARNESS.md` and the *Saved shapes of work*
pages all mean **a saved program built out of this binary's own node kinds**
(`agent.loop`, `tool.call`, `verify`, `human.gate`…), designed in the
conversation, stored under `~/.codeaf/harnesses/<name>/vN.json`, reached by
cue detection rather than by command. An outside binary that runs its own
agent loop is the opposite thing: codeaf designs nothing about it and cannot
see inside it. Calling both "sub-harness" would put two objects behind one
word in the manual, and the manual is what the chat answers from.

So: **delegate**. A person "delegates the auth rewrite to swe-pro". The manual
page is *Delegates — programs codeaf can hand a task to*.

## Why not `/swe-pro <prompt>`

Three reasons, each already a law somewhere in this repository.

1. **The turn must not wait.** A swe-pro run is thirty to ninety minutes. A
   turn that blocks on it holds the conversation, the status line and the
   person hostage, and the engine's own thirty-minute idle retirement (#1291)
   is written on the assumption that long work is *work in the tree*, not a
   turn. codeaf already has the right shape: work leaves the conversation as a
   task, the turn ends, and the landing **wakes** a turn that reads the result.
   A delegate ends the same way — the model never watches the stream; it
   reads the terminal record and the landing note when it is woken.
2. **One door for work you walk away from.** `/task` is that door, and the
   tasks page, the rail, `stop`, the working copy, the landing card and the
   spend folding all hang off it. A `/swe-pro` command would have to rebuild
   every one of those or ship without them. The delegate rides `/task`.
3. **The model should be able to choose it.** A person who types
   `/swe-pro` has decided; the more useful case is the model proposing "this
   one is big enough for swe-pro" from inside an ordinary turn, which means
   the choice has to be a field on the task proposal, not a command.

What a person types, then, is one of:

```
/task via swe-pro rewrite the auth middleware to use the new session store
/task rewrite the auth middleware …        ← the model may propose a delegate itself
```

and `/delegate` (bare) lists the delegates this machine has, the way
`/subharness` lists programs. `/delegate <name> <words>` is an alias for the
first form, kept because a one-word command is what a hand reaches for.

## What already exists, and where this plugs in

The run engine's worker contract is one method:

```go
// internal/run/worker.go
type Worker interface {
    Run(ctx context.Context, task plandb.Task) (Report, error)
}
type Report struct { Result string; Steps int; USD float64; Waiting bool }
type WorkerFactory func(task plandb.Task) Worker
```

Everything the person sees and every limit they set already reaches a worker
through the supervisor: the cost ceiling and the elapsed ceiling
(`run.Limits`), the spend bank a worker reports rising dollars into
(`run.WithSpendBank`), the live step the rail draws (`plandb.Store.SetLive` /
`ClearLive`), the trajectory the task page opens (`trajectory.jsonl`), the
working copy cut per run (`task_run_copy.go`), landing and merge
(`run.Land`, `landBeltRun`), the stop road (`session.Cancel` with a kind, and
`stoplaw_test.go` proving every running row can be stopped), and the fold of
the run's dollars into the conversation's total (`driveBeltRun`'s
`foldSpend`, #1280).

**A delegate is a second implementation of `run.Worker`.** `CrewFactory`
already chooses a worker per task by role; it grows one more branch: a task
whose row names a delegate gets a `delegate.Worker` instead of a
`BashWorker`. Nothing above the factory changes.

The floor to measure against is what works today with no change at all: the
model runs `swe-pro run …` through the `bash` tool with `background: true`.
That gives a job with a ring-buffer log and an exit notice on the owed lane —
and no working copy, no landing, no dollar limit, no rail row, no cost in
`/cost`, and a model that has to poll the job to find out. That gap is the
whole of what this design pays for.

## The delegate contract

codeaf asks five things of a program before it will hand it a task. They are
written as a manifest, one per delegate, and the manifest is the whole of
what codeaf knows about the program.

| the program must | swe-pro today | `codeaf do` today |
| --- | --- | --- |
| **launch** from argv with the task as text, a working directory, a dollar ceiling and a wall ceiling | `swe-pro run --dir D --max-cost X --max-hours H -- "goal"` | `codeaf do --workspace D --max-cost X -timeout H --json "brief"` |
| **stream** its progress as one JSON object per line on stdout and nothing else | yes (EVENTS-CONTRACT.md) | no — stdout is the result only; progress is prose on stderr |
| **end** with exactly one terminal record carrying a status word, a reason and `cost_usd` | yes, `{"type":"terminal",…}` | the `--json` envelope, one object, on exit |
| **stop** cleanly on SIGTERM, still writing its terminal record | yes; SIGKILL is the only way to lose it | yes, the exit ladder |
| **leave its work in the tree** it was given, as commits or a dirty tree, and nothing that is not its work | eager `wip(edit)` commits; `.swe-pro/` git-excluded; `refs/swe-pro/*` | commits on a branch it names in the envelope |

Two things codeaf does **not** ask, and says so on the page:

- **Questions.** A delegate cannot ask the person anything. swe-pro's
  `question` tool is auto-rejected inside the binary and there is no stdin
  road; codeaf's own headless door exits `4 needed an answer` for the same
  reason. The task brief has to be self-sufficient, and the page says so in
  those words.
- **A step cap.** swe-pro has cost and hours and nothing per step.
  `Limits.StepsPerTask` is not handed down, and the task page's step count is
  whatever the adapter can read off the stream (tool parts for swe-pro,
  nothing for `codeaf do`).

### The manifest

```jsonc
// ~/.codeaf/delegates/swe-pro.json — read at launch; absent binary = absent delegate
{
  "name": "swe-pro",
  "description": "an autonomous coding agent for one large, well-specified change",
  "bin": "swe-pro",                                  // resolved on PATH; a path is allowed
  "argv": ["run", "--dir", "{{workspace}}",
           "--max-cost", "{{cost_usd}}", "--max-hours", "{{hours}}",
           "--", "{{brief}}"],
  "env": { "OPENROUTER_API_KEY": "{{key:openrouter}}", "SWE_PRO_CP_URL": "off" },
  "reader": "swe-pro",                               // which stream reader (below)
  "limits": { "cost": true, "elapsed": true, "steps": false, "questions": false }
}
```

`{{key:openrouter}}` is resolved through `config.APIKeyAt`, the same door
every lane and the e2e suite resolve a key through, so a key pasted into
first-run setup reaches the delegate (#576 is the lesson). A manifest whose
`bin` is not on PATH means the delegate is not offered — A CAPABILITY THAT
CANNOT WORK IS ABSENT, NOT BROKEN — and `/delegate` draws one dim line naming
the binary it looked for.

### The readers

A reader turns the program's stream into the three things the supervisor
wants while the program runs — a rising dollar figure, a live step sentence,
a trajectory line — and, at the end, a `Report` and an outcome word. Readers
are Go, in the binary, one per stream shape; a manifest names one. Two ship:

**`swe-pro`** reads EVENTS-CONTRACT.md:

| stream | becomes |
| --- | --- |
| `stage`/`status` compact records | the live step (`implement · running`, `verification · pass`) and one trajectory line each |
| `message.part.updated` with `part.type == "tool"` reaching `completed`/`error` | a trajectory step: the tool and its command, the observation head; `Steps` counts these |
| `message.updated` for an assistant message with `cost` | banked spend: the sum over completed assistant messages, monotonic |
| `terminal` | the `Report`: `Result` from `message` plus `data.reason` and `data.submission_reason`; `USD` from `data.cost_usd`; the outcome word from `status` |
| process exit with no terminal read | `ran and did not finish`, with the last stage seen in the result |

The outcome mapping, written once beside the reader:

| swe-pro `terminal.status` | run outcome | rail word |
| --- | --- | --- |
| `pass` (`data.status` pass or pass-unverified) | done | done |
| `fail` (`data.status` fail or unsubmitted) | ran and did not finish | incomplete |
| `budget-exhausted` | a limit you set stopped it | stopped, naming the limit (#1279) |
| `crashed` | ran and did not finish | incomplete |

The model's claim and the harness's own observation stay separate fields in
swe-pro's record and they stay separate in the landing note: *swe-pro says it
submitted; its verification failed 2 of 5 commands* is two sentences, never
one.

**`codeaf`** reads the `--json` envelope on exit and the stderr quiet line
while it runs. It exists so that `codeaf do` on a second machine, over the
ssh road `--host` already has, is a delegate with no new machinery — and so
the contract is proven against a program whose stream discipline we control.

### Money

The delegate spends the person's key outside codeaf's provider ledger. The
supervisor's bank (`bankLive`, `countLiveSpend`) sees every rising figure the
reader hands it and cuts the run at the ceiling the way it cuts a bash worker
— by cancelling the context, which sends SIGTERM, which lets swe-pro write its
terminal record. The conversation's total, `/cost` and the status line move
through `foldSpend` as they do for any run (#1280). The machine's usage ledger
on disk does **not** get swe-pro's lines, because those calls were not made
through a codeaf lane; the spending page says `via swe-pro` on the row rather
than pretending otherwise.

The dollar ceiling handed to the program is what is left of the smaller of
the conversation's limits (`runCostLeft`, #1281), passed on the command line
so the program cuts itself before codeaf has to.

### Stopping

`stop` on the row is `session.Cancel` with a new kind, `delegate`, and it goes
on the ledger in `stoplaw_test.go` with its proving test, like every other
kind. The worker terminates the process group (`internal/processgroup`), waits
the job grace (`jobTermGrace`), then kills. A terminal record that arrives
inside the grace is read and folded; one that does not leaves the row
`stopped` with the last stage seen.

### Landing

The working copy is the run's, cut as it is today. swe-pro is given it as
`--dir`. When the program ends, `run.Land` commits what is in the tree and
`landBeltRun` merges it home exactly as for a bash worker. Two swe-pro
particulars the landing has to know:

- swe-pro's eager `wip(edit): path` commits are history in the copy. Landing
  keeps them (one merge, honest history) rather than squashing — a person
  who wants one commit has the branch. This is a decision to take, not a
  fact; the other answer is one squash commit whose message is the terminal
  record's reason.
- `.swe-pro/` is in `.git/info/exclude` of the copy, so it never lands, and
  `refs/swe-pro/start` and `refs/swe-pro/submitted` die with the copy.

### What the model is told

`propose_task` grows an optional `via` field naming a delegate, and the
system prompt's `HANDOFF_FACTS` names the delegates this launch has, in the
same conditional way it names everything else (`beltfacts.go`): a build with
no manifest and no binary says nothing about delegates at all. The fact says
when to choose one — *a change big enough to want its own agent for an hour,
specified well enough that nobody will be asked anything* — and the model
proposes it on the same card `/task` shows, with `via swe-pro` on the card,
so the person still answers before money moves.

## What has to change in swe-pro

These are on the swe-pro side, and none of them is codeaf's to work around.

1. **A standalone mode.** `swe-pro run` refuses to start without an AgentField
   control plane answering `/health`; the message says "cannot be used
   standalone". A codeaf user has no plane. The seam already exists — the
   `injected` backend path tolerates a failed probe and continues with the
   plane disabled — so this is `SWE_PRO_CP_URL=off` (or `--no-control-plane`)
   taking that same branch, and a `run-contract` record that says the plane
   is off.
2. **Cost as a compact record.** Live cost is today only recoverable by summing
   `message.updated` assistant `cost` fields. A `{"type":"spend","cost_usd":…}`
   compact record after each model request, cumulative, would make every
   consumer's live limit exact and free the reader from the bus schema.
3. **A question road, later.** `question.replied` is defined "for embedders
   that answer". If codeaf ever answers a delegate's question from the rail
   (the way it answers a worker's note), swe-pro needs a stdin or socket road
   to deliver it and to stop auto-rejecting when one is attached. Not v1.

## What has to change in codeaf

| # | lands | proof |
| --- | --- | --- |
| **1** | `internal/delegate`: the manifest and its loader; `Worker` (spawn under `processgroup`, stream to reader, SIGTERM-then-kill, `Report`); the `swe-pro` reader; the `codeaf` reader | unit tests against a fake binary that emits scripted NDJSON and honours SIGTERM; the outcome table pinned |
| **2** | the door: `plandb` task row carries `via`; `CrewFactory` branches on it; `/task via <name>`, `/delegate`, `propose_task.via`; `HANDOFF_FACTS`; the cancel kind and its ledger line; the landing note's two sentences; the spend row's `via` | focused `internal/session` and `internal/tui3` tests; the manual gates |
| **3** | the manual: *Delegates* page (what one is, how to ask, what it cannot do — no questions, no step cap — what it costs, where the work lands, the refusals verbatim); the `commands.md` rows | `internal/manual/chat_test.go` probes in a person's words: "can you hand this to swe-pro", "delegate this", "why can't the delegate ask me" |
| **4** | hosted: the row crosses `internal/remote` (`PlanTaskRow` already carries `Live` and `TrajectoryPath`, so this is mostly the `via` word); until then a `--host` session refuses with one sentence, the way `/subharness` does | `internal/remote` wire tests |
| later | the model chooses a delegate by seat (`worker` seat → swe-pro for `work` leaves, a crew row); a delegate on another machine; answering a delegate's question | — |

Wave 1 has no door and spends no money; it is the contract, proven against a
stub. Wave 2 is the first thing a person can type.

## Open questions

1. **Squash or keep** swe-pro's eager commits at landing (above).
2. **Who picks the delegate's models.** swe-pro's `--high` pool is its own
   default today. The manifest could pass the conversation's work seat
   (`{{model:work}}`) so `/crew` governs the delegate too — but swe-pro speaks
   OpenRouter slugs and codeaf's seat may be on another lane. First cut: the
   manifest's own argv, no seat.
3. **Is `via` on the task or on the run?** A run is one store; a delegate is
   one process that owns the whole tree for the hour. First cut: a delegated
   task is a run of one task, and the supervisor never splits it. Splitting
   a run between bash workers and a delegate is a later question.
4. **Trajectory from a foreign stream.** The task page assumes a step is a
   command and an observation. swe-pro's tool parts fit; its `stage` records
   do not. Either the page learns a "stage" row or the reader folds stages
   into the live step only and never into the trajectory.

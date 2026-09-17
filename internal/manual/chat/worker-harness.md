# The worker harness

## What happens when I type /task

On this road, `/task <brief>` starts a **run** rather than a node of the
conversation's own tree. It says so in one place and nowhere else — the switch
below — and the older road is what a build without the switch does.

Typing `/task` asks you nothing and waits for nothing in front of it: the work
exists as soon as you press enter. What it does instead is:

- **open or join the conversation's plan store** — a `plandb` database that is
  the run's whole plan;
- **seed the work** from your own sentence under the run's root row, and hand the
  store to the run engine in a goroutine;
- **answer at once** with the id the store knows the work by, so the conversation
  stays usable while the run goes. The run's own page is the store's root.

**A second `/task` joins the live run.** One store is one run, so the new work
becomes a child of the run's root rather than a second store, and the supervisor
already turning picks it up on its next pass.

When the run ends, the store's root carries the outcome and the landing, and the
conversation is woken with the same note a landed task sends — the outcome word,
the result the root reported, and where the work went (`landed on <branch>: N
files`, or the sentence saying why it did not). The row the run was published
under settles `done` when the run finished whole and `incomplete` on any other
ending.

**With the switch unset, none of this is reached.** `/task` raises an ordinary
task on this session's own tree, briefed beside its worker and landed through the
task graph. See *How to turn it on*.

## The tasks pane and a task's page

While a run is live the task pane draws its **plan**: one row per task in the
store, in the place's own row machinery, so a plan row looks like every other row.
Each row wears one state word, mapped off the store's own status:

- `running` — the store says `pending`, `ready`, `claimed` or `running`: the work
  is deliverable, or a worker has it.
- `done` — the store says `done`.
- `incomplete` — the store says `failed` or `cancelled`. Nothing judged it, so the
  word must not send you looking for a fault.
- `your call` — the store says `paused`: the task is held at a gate, which is your
  call and nothing else's.

Beside the word a row may carry the steps its worker recorded and the dollars its
spend rows hold — each left out when it is nothing.

**`enter` opens the task's page.** It is built from the store's own read and is the
same full frame, the same `esc`, and the same way back as a record row's card. It
shows, in order, each section left out when nothing is behind it:

- `description` — the work order the worker was given;
- `notes` — every note left on the task, with its author, your own reading `you`,
  and its moment;
- `steps` — the trajectory its worker recorded: each command with the head of what
  came back, the whole observation on disk behind the row.

A page the engine will not answer for — a task this conversation did not spawn, or
one whose store has gone — is not opened; the list stays where it was.

## Steering a task: notes, pause, cancel, amend, priority

The person's door onto a run's plan is six verbs, each resolving an id **inside
this conversation's plan**, so a task another chat spawned is never reachable:

- **note** — a note in your own voice on one task, which the worker reads in its
  next frame. On a plan task's page it is what the composer sends: type in it and
  press `enter`, under the placeholder `a note for this task`. It is not a chat
  turn — the words go to the store and never to the model.
- **pause** / **resume** — hold a task and everything under it out of the ready
  frontier without changing its rung, so running steps finish and nothing new in
  the subtree is launched; or release the hold. The key is `p`: a running row
  reads `p pause` and a held one `p resume`.
- **cancel** — end a task, its descendants and the work hard-depending on it. The
  key is `x stop it` (the roster's own cancel key), on the row and on the page.
- **amend** — prepend text to a task's description, the way the CLI's `task amend
  --prepend` does, so the plan learns while it runs.
- **priority** — set a task's priority through the store's revision verb.

`x` and `p` are read only over an **empty box**: the moment there is a note to
type, a letter is a letter.

Two refusals are this layer's own, and they are the words the pane reads back:

- `no task <id> in this conversation`
- `that task belongs to another conversation`

A refusal the store itself answers travels back as the store wrote it, because the
store is the one that knows its own laws — the root is the harness's, a task that
has ended cannot be cancelled, and a revision is only for work that has not
started.

## What a worker can do — the one shell a task worker can actually run

A harness worker's belt carries **one shell hand, `bash`**. The hands other workers
reach for as tools — `read`, `edit`, `write`, `grep`, `find`, `ls` — are shell
commands here, and each command runs in its own fresh shell, so a `cd` does not
outlive it: chain the directory in (`cd dir && …`).

Coordination runs through **`plandb`**, the plan CLI, in that shell. The plan is one
database for the whole run — what you add, what a sibling adds and what the runtime
starts are the same list. The worker uses `plandb add`, `plandb split`, `plandb task
note`, and the reading set `plandb task overview`, `plandb show`, `plandb list
--status ready`, `plandb critical-path`. The lifecycle verbs are the runtime's, and
finishing its own task goes through `plandb done --agent <name> --result '…'`.

Four `codeaf` doors reach the belt's non-shell hands from a shell, each through the
same code path the tool runs, so the two cannot drift:

- `codeaf patch FILE --old TEXT --new TEXT` — the edit hand's exact-match replace.
- `codeaf doc PATH [--pages A-B]` — a document the way `read_document` reads it.
- `codeaf web fetch URL` / `codeaf web search QUERY` — the belt's web verbs.
- `codeaf image PROMPT --out PATH` — one picture the way `generate_image` makes one.

A few hands a shell cannot be are kept too — the billed `read_document`, `jobs`,
`manual`, and the web, media and services families.

**A worker cannot ask you a question.** `ask` is not on its belt: the loop reaches
the person through the plan CLI and not a consent gate, so a thing it cannot have
answered it answers by **re-planning**. The verbs a node worker has for handing
work out — the task graph's own — are off this belt for the same reason: a belt
carrying both would teach two ways to say one thing.

## Costs and limits

- **The cost cap.** A run may spend what the conversation's own **spend rail**
  allows — `/settings` → Spending → **per conversation** — which is off by default.
  The run's width and its dollar ceiling are the conversation's own numbers, so a
  run costs what the conversation costs and runs as wide as the conversation may.
- **The step cap.** A worker stops at **200** finished tool calls per checkpoint,
  the same figure a node worker carries, backstopped at 1000 (200 × 5). At a
  checkpoint a second look runs: progress buys another 200 steps.
- **Spend rows by seat.** Every call a run makes lands one row in the plan store's
  ledger, tagged with the task, the model, and the role — the **seat** — it ran on.
  Read it back with `plandb spend`, by role and by model, or rolled up under one
  axis: `plandb spend --by seat` (also `chat`, `project`, `model`, `task`), with
  `--since 7d` to bound the window.

## Headless: codeaf do — the exit code it leaves with

`codeaf do "<task>"` runs one job with nobody watching, then exits. On this road it
is dispatched by the run engine over the project's own plan store — the same
worker, the same store and the same exit ladder — rather than by the resident's
reconciler. `--json` prints one machine-readable object either way: the deliverable,
then `files:`, then `learned:`, then one footer line; the sentence goes in `error`
when it could not be run at all.

**Every headless verb leaves on one ladder**, and this is what `$?` holds:

```
0  it is done, and what is on stdout is the answer
1  it could not be run at all — no key, bad arguments, the store would not open
2  it ran and did not finish: part of the work does not stand
3  a limit you set stopped it — the wall, the token budget, the turn cap, the price
4  it needs an answer from you and nobody was there
```

`stop` on the `--json` object is the same vocabulary's word for which rung's reason
it was (`done`, `error`, `incomplete`, `unchecked`, `budget`, `turn-cap`,
`deadline`, `price`, `question`), and `ok` is true on exactly the runs that leave
with 0.

## How to turn it on

**This page describes machinery that is not the shipped default.** The harness runs
when the environment variable `CODEAF_TASK_BELT=bash` is set. With it unset, the
build is unchanged and the older engine serves every road:

- a `/task` is a node of this session's own tree, not a run on the plan store;
- a task worker carries the conversation's own tools, not one shell;
- `codeaf do` is dispatched by the resident's reconciler, not the run engine.

The switch is read where the belt is composed, where a person's `/task` is
admitted, and where `codeaf do` chooses its road — and **with it unset, not one
byte of any prompt, belt or landing moves**.

Everything behind the switch is a seam. A build with no run engine linked answers
the older road, and every refusal on the run road falls back to it rather than
inventing a sentence of its own — so a conversation the run road cannot serve gets
exactly the door it always had.

# The worker harness

## What happens when I type /task

On this road, `/task <brief>` starts a **run** rather than a node of the
conversation's own tree. Everything here hangs on one switch, named under *How to
turn it on* below, and the older road is what a build without it does.

Typing `/task` asks you nothing and waits for nothing in front of it: the work
exists as soon as you press enter. What it does instead is:

- **open or join the conversation's plan store** — a `plandb` database that is
  the run's whole plan;
- **seed the work** from your own sentence under the run's root row, and hand the
  store to the run engine in a goroutine;
- **answer at once** with the id the store knows the work by, so the conversation
  stays usable while the run goes. The run's own page is the store's root.

**A run works in a copy of its own, never in your folder.** The copy is cut from the
folder the task is about, as that folder stands, uncommitted edits and untracked files
included, so you can keep working in yours while the run goes. Every part of one run
works in that one copy.

**A second `/task` joins the run already underway** when it is about the same folder:
the new work becomes a child of the run's root, shares the run's copy, and its answer
says `It joined the work already underway and shares its copy.` A proposed task about
ANOTHER folder is refused while that run is underway, with both folders named and
`tasks that run together share one copy of one folder. Propose it again when that work
has ended`. A task handed off after the run has ended starts a run of its own, in a new
copy cut from your folder as the first run left it.

**When the run ends its work comes home by itself.** The copy's work is committed and
merged into the folder it was cut from, the copy is given back, and the run's page
carries `its work is in <folder> on <branch>`. The conversation is woken with the same
note a landed task sends: the outcome word, the result the root reported, and where the
work went (`landed on <branch>: N files`, or the sentence saying why it did not). Work
that will not go in is never forced: the branch is kept in your repository and the note
names it, for example `its branch <branch> was kept`, when your checkout moved on after
the copy was cut. A run that only read says `nothing to land: the run's working copy holds no change` and changes no file. The
landing card says `merged` when the work is in your folder and `branch kept` only for a
branch that is waiting. A hand-off that joined the run ends with it: its row settles
`done` or `incomplete` when the run's does. The
row the run was published under settles `done` when the run finished whole and
`incomplete` on any other ending.

**With the switch unset, none of this is reached.** `/task` raises an ordinary
task on this session's own tree, briefed beside its worker and landed through the
task graph. See *How to turn it on*.

## The tasks pane and a task's page

While a run is live the task pane draws its **plan**: one row per task in the
store, in the place's own row machinery, so a plan row looks like every other row.
Each row wears one state word, mapped off the store's own status:

- `queued` — the store says `pending`: the work is admitted and not started, with
  nothing in its way but a slot. A row held behind named work says what is holding
  it: `queued · waits: <the work it hangs under>` — see *Why does it say queued?*.
- `running` — the store says `ready`, `claimed` or `running`: the work is
  deliverable, or a worker has it.
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
- `notes` — every note left on the task, with its moment. A note you left reads
  `you`. A note a worker or the run left names no author: the store knows those
  only by ids of its own, and an id is never drawn on this page;
- `steps` — the trajectory its worker recorded: each command with the head of what
  came back, the whole observation on disk behind the row.

A page the engine will not answer for — a task this conversation did not spawn, or
one whose store has gone — is not opened; the list stays where it was.

## Open a run's task from the side list — click its row, or one of its parts

With the switch on, a run is drawn in the conversation's side list as its own row,
`#N`, with its parts and their checks hanging under it. Every one of those rows is a
door: click the run's row, or select it and press `enter`, and its page opens over the
conversation; click a part's row or a check's row and THAT task's page opens. The page
is the one the tasks place opens: what the task was asked, its notes, its steps, and the
box that leaves a note. `esc` goes back to the conversation exactly as you left it, with
whatever you had typed still in the box.

A page opened on a task that is still running follows it, one read at a time, and stops
reading when the task has settled. A step whose command is many lines long is drawn as
its first line and `…`; what ran is unchanged.

A row the store has no page for opens what it always opened, its room. That is every
task when the switch is off, and a task of an earlier run once a later run has started.

## Why is this task indented under that one?

The pane draws the run's **plan as a tree, not a flat list**. A task sits under the
task that requested it — the parent the worker wrote to the store — and a deeper
task sits under that, each joined to the row above it by the pane's own connector
(`├ `, `└ `). The shape of the run reads down the indentation.

A dependency never changes that family. `pending` means admitted and not started;
the row stays under the task that requested it and wears
`queued · waits: <that task>` to name the separate dependency.

A task's **page** shows its children under its steps the same way, each with its
live step while its worker is on one. Notes, pause, cancel and the rest of steering
are unchanged by the tree.

## What step is a run task on?

A run's task row carries the step its worker is on **right now**, under its title:
the running glyph `◐`, the shell lead `$` and the command that step is running; under
that, the task's own figures — how many steps its worker has taken and what it has cost
— joined ` · `:

```
 ◐ Add rate limiter to /api/upload
   $ git grep -n RateLimit internal/api
   12 steps · $0.11
```

Each half of the figures is left out when nothing is behind it, so a step in flight on a
task that has recorded no step yet draws the command alone. The command gives up its tail
to the column's width; the glyph and the `$` are never spent on it.

**The line is there only while a step is in flight.** A task that has not started, one held
behind named work, and one that has landed all draw their ordinary row and no live line — the
store clears the step the moment its command ends. These rows are a run's **plan rows**, drawn
in the tasks place (`/history`, `ctrl+.`, `alt+2`, and the roster raised over the frame), not
on the always-on column, which draws this conversation's own tree.

## What a run task's page shows while it runs

`enter` on a run's row opens the task's page, and while the task is running the page follows
its newest step: it re-reads itself on the clock and stays stuck to the bottom — the newest
step in view — until you scroll up, which releases it. Scrolling back to the bottom takes the
follow up again without your pressing anything.

The step being run right now is drawn **one step early**, in the page's `steps` section: the
running glyph beside `$ <command>` in place of the number the record will give it, and, once
the call has been open ten seconds, its own clock dim under it:

```
running · 12 steps · $0.11
description
  Add a per-IP rate limiter to the upload handler; …
steps
  11  $ sed -n 40,120p internal/api/upload.go
  12  $ git grep -n RateLimit internal/api
      3 hits
  ◐  $ go test ./internal/api/...
      running 41s
```

When the command ends the store clears the live step and the next read draws it as an ordinary
step, with its number and the head of what came back.

## Why does it say queued?

`queued` is this surface's own word for a run task the store holds `pending`: the work is
admitted and not started, with nothing in its way but a slot. It is **not** `running` — a task
still waiting for its turn has nothing in flight, and the row says so rather than borrowing the
running word.

**Held behind named work, the row says what holds it.** A task the store keeps `pending` until
its own hard dependencies and every ancestor's are done reads `queued · waits: <the work>` — the
name is the row it hangs under, and it is a title and not an id, so a task whose parent this page
has never heard of, one with no words on it, or one that has already landed draws the bare word
`queued`.

## Does a subtask see my original request

Yes — every worker that is not the run's root reads your sentence again, word for
word, in a section of its page headed *The ask this run serves*. The planner's work
order is only that worker's one part of it, and where the two disagree about that
part your words win — a worker that had to go against them says so in its report
rather than quietly choosing.

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
type, a letter is a letter. A task that has ended, `done` or `incomplete`, is offered
neither: its row and its page name no `x stop it` and no `p pause`, because the store
would refuse both.

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

## How does a task finish, and what if it is blocked?

A worker ends a task one of three ways, and **a reply is not one of them**. It
acts with a bash call; it **finishes** with `plandb done --agent <name> --result
'…'` on its own task, and only once the acceptance holds; or it **parks** with
`plandb wait` when it is blocked on a dependency or a child. A reply that runs no
command — "now writing the parser:" — changes nothing and does not end the task:
the harness answers it in the belt's own voice — `no action executed: answer with
one bash call; finish with plandb done <your id> --result '…' when the acceptance
holds; wait with plandb wait when you are blocked on another task` — and the
worker goes on. Four such replies in a row fail the task, and the task's record
ends with the reason `4 replies in a row carried no action`.

A parked task stays open and not done, and its claim is released: the runtime runs
its worker again **once, when its wait is over** (see the next section), with the
finished work in front of it. The woken task is claimed under its own agent name
again, so it can re-plan, add another child and park again, or finish with `done`
exactly as it could on its first launch. A `plandb wait` with nothing open to wait
on is refused, so a worker cannot park on nothing. The remaining endings are the
run's step cap, its wall, and an errored turn.

## When does a waiting task come back?

A parked task comes back **once, when its wait is over** — and "over" is one of
two facts, nothing else:

- **Nothing it waited on is open any more.** Every dependency is done and every
  child it parked on has finished; the task is launched again with all of their
  titles, statuses and results in one clause, so it integrates the whole set
  rather than only the last thing to land.
- **One of them failed or was cancelled.** The task is launched again at once,
  with that one's ending named, so it can re-plan instead of waiting on siblings
  that are still running.

A child merely being **claimed, started, noted or otherwise touched** while a
sibling still runs is **not** a reason to come back, and neither is a dependency
stirring without landing. The park is a wait on something, and it is over when
that something has finished — not when it has moved.

## How does a task decide it is done?

Finishing is not reaching the end of the work — it is proving every requirement
of it. Before it runs `plandb done`, a worker walks each requirement sentence of
its own work order, and of the ask the run serves, one per line, and names
beside each the command or test that proved it in that run. A requirement with
no proof is not done: the worker proves it then, or reports it undone in its
result. Reaching the end of the steps is not the same as having met every
requirement in them, and speed is no permission to skip the walk.

## Costs and limits

- **The cost cap.** A run may spend what the conversation's own **spend rail**
  allows — `/settings` → Spending → **per conversation** — which is off by default.
  The run's width and its dollar ceiling are the conversation's own numbers, so a
  run costs what the conversation costs and runs as wide as the conversation may.
- **The step cap.** A worker stops at **200** finished tool calls — the same
  figure a node worker carries. The cap is a bound on spend and not a finding about
  the work: the turn is stopped there rather than judged, and a worker stopped this
  way did not finish.
- **Spend rows by seat.** Every call a run makes lands one row in the plan store's
  ledger, tagged with the task, the model, and the role — the **seat** — it ran on.
  Read it back with `plandb spend`, by role and by model, or rolled up under one
  axis: `plandb spend --by seat` (also `chat`, `project`, `model`, `task`), with
  `--since 7d` to bound the window.
- **Thinking level.** A run worker answers at **low** reasoning. Its belt is one
  action per response, so the depth you configured would be paid again on every
  round of the run. The seat is a floor and not a cap: a rung set on the task, on
  the conversation or on the turn still wins.

## Which model does my task use?

Every task a run launches sits in one of two **seats**, and each seat is a model
named on a door or in the profile:

- **`--model` is the work seat** — the model a leaf that does the work itself
  runs on. `codeaf do` reads it from `--model`, then `CODEAF_MODEL`, then the
  profile's crew, then this build's default; a `/task` in a conversation reads it
  from the conversation's own worker row. A root is born a leaf, so its first
  launch rides this seat, and so does every task the plan adds under it.
- **`--plan-model` is the plan seat** — the model the root and every task that
  has children run their coordinating turns on. `codeaf do` resolves it the same
  way from `--plan-model`, then `CODEAF_PLAN_MODEL`, then the crew; a
  conversation takes it from its mastermind row. A leaf that splits moves onto
  this seat for the turns where it is a coordinator.
- **`--check-model` is the check seat**: the model a check the review round
  adds reads a finished leaf against. `codeaf do` resolves it from
  `--check-model`, then the `CODEAF_CHECK_MODEL` environment value, then a plan
  seat pinned by `--plan-model` or `CODEAF_PLAN_MODEL`. A run pinned to two models checks on
  the plan seat and no third model appears from the profile. Without those pins,
  the check takes the crew's careful row, the same row a conversation's checker rides. The
  **probe** seat is the one the profile's own `low` row answers alone: nothing
  on a door names it, so a probe runs on the crew you set in `/crew`.

The seat a person names is the seat **every** launch takes — a task launched
after the door resolved the seats still runs on them, not on whichever row the
profile happens to hold. Read it back with `plandb spend --by seat`.

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

## How do I tell the check what to run?

Declare each proof command when the task is created: add `--check '<the command
that proves it>'` to `plandb add`, and repeat `--check` when the task has more than
one command to run. The check runs every command exactly as declared before it
probes any acceptance sentence those commands do not cover.

A task with **no declared check** is checked by reading its result and by the
acceptance alone. Commands mentioned only in the task's prose are not declarations,
so put every command the check must run on the task with `--check`.

## Who checks a task's work?

Every leaf that lands **done** is checked, at both doors — `/task` and `codeaf
do`. The run adds one **check** task under the leaf's parent, on the plan seat,
carrying the leaf's acceptance and the result it reported, and the run's
completion waits on it like on any other child: a run is not over until its
checks have landed. A run whose root did the work alone is checked the same way
before it finishes.

A check does not redo the work. It reads the acceptance sentence by sentence,
runs the leaf's own tests, and probes each sentence the tests do not cover. It
finishes with one line, in one of two shapes:

- `holds: <one sentence saying why>` — the acceptance is met.
- `does not hold: <the one unmet requirement, and the command that showed it>` —
  the acceptance is not met.

**A `does not hold:` finding is work, not a remark.** The checked task keeps its
done ending and the sentence is left as its note, and the run adds a `fix:` task
under that task's parent — carrying the acceptance, the finding and the result —
which must land before the run is over. The fix is checked in turn, but only
once: a finding on a `fix:` task is a note and no second fix task, so a run
cannot loop.

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

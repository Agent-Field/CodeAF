# The worker harness

## What happens when I type /task

On this shipped default road, `/task <brief>` starts a **run** rather than a
node of the conversation's own tree. *How to turn it off* below names the
switch to the older road.

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
copy cut from your folder as the first run left it. A task handed off in the few seconds
while a run is finishing (its work landing, its summary being written) waits until that
run is over and then starts its own: it never joins a run on its way out.

## When does a task run's work come home, including commits its workers made

When a `/task` run on the worker harness ends, its copy's uncommitted work is committed and
merged into the folder it was cut from, the copy is given back, and the run's page
carries `its work is in <folder> on <branch>`. The conversation is woken with the same
note a landed task sends: the outcome word, the result the root reported, and where the
work went (`landed on <branch>: N files`, or the sentence saying why it did not). Work
that will not go in is never forced: the branch is kept in your repository and the note
names it, for example `its branch <branch> was kept`, when your checkout moved on after
the copy was cut. A run whose workers committed everything still names its
branch and changed files; codeaf signs those commits before the work comes home.
A run that only read says `nothing to land: the run's working copy holds no change`
and changes no file. The
landing card says `merged` when the work is in your folder and `branch kept` only for a
branch that is waiting. A hand-off that joined the run ends with it: its row settles
`done` or `incomplete` when the run's does. The
row the run was published under settles `done` when the run finished whole and
`incomplete` on any other ending.

**With the switch unset, this is the road `/task` takes.** Set
`CODEAF_TASK_BELT=node` to use the older session tree road instead. See *How
to turn it off*.

## The tasks pane and a task's page

While a run is live the task pane draws its **plan**: one row per task in the
store, in the place's own row machinery, so a plan row looks like every other row.
Each row wears one state word, mapped off the store's own status:

- `queued` — the store says `pending`: the work is admitted and not started. A
  row held behind named work says `queued · waits: <the work it hangs under>`;
  a ready row held by the machine says `queued · machine busy`.
- `running` — the store says `ready`, `claimed` or `running` and there is no
  machine hold: the work is deliverable, or a worker has it.
- `done` — the store says `done`.
- `incomplete` — the store says `failed`, or `cancelled` by anything but your own
  stop. Nothing judged it, so the word must not send you looking for a fault.
- `interrupted` — the run was set aside because nothing was driving it when the next
  task arrived (see *Does a new task pick up a run that did not finish?*). Not a fault,
  and not a stop of yours.
- `stopped` — you ended it. A run you stopped, a part you stopped with `x`, and every
  part that stop ended with it all read `stopped`, on the side list, in the tasks
  place and on the task's own page alike. A part that had already failed, or that the
  run cancelled for its own reasons at another moment, keeps `incomplete`.
- `your call` — the store says `paused`: the task is held at a gate, which is your
  call and nothing else's.

Beside the word a row may carry the steps its worker recorded and the dollars its
spend rows hold — each left out when it is nothing.

**`enter` opens the task's room**, the same room every task opens on either engine:
the same full frame, the same head, the same `esc`, the same box and the same two tabs.
The only difference is where it reads from: a run's task is read from the run's store.

- The head is a task room's head: the trail with the way back at its far end, then
  the task's name with its state word, how long it has run, what it has cost, and the
  tokens its worker read and wrote. Each figure is left out when the store has not
  got it. The model its worker spent the most through is shown with the task's setup
  on the right. A task whose store holds no spend for it shows neither.
- The `transcript` tab opens on the work order the worker was given, then the
  trajectory its worker recorded: each command that ran is the room's own shell
  call, folded the way any task's calls are folded, with what came back
  behind it. The step in flight is the newest call. A call known not to have run
  stays in the record and in the step count and is never drawn as a call: a command
  the worker tried and was refused is one dim line, `refused` and the command, and
  any other has no row. Then the notes left on the task: a note you left is drawn
  where your own corrections are drawn, and a note a worker or the run left names no
  author, because the store knows those only by ids of its own.
- Under the transcript, the tasks it hangs on (`waits`) and the tasks under it
  (`under it`), each drawn as the side list draws a task. A press on any of those
  rows opens that task's room.
- The `work` tab is the run's working copy: what every task of the run changed in
  it against the commit it was cut from, and the files it added. The files codeaf
  itself keeps there are left out. `tab` over an empty box moves between the two
  tabs, and a click on either name does the same.

A page the engine will not answer for — a task this conversation did not spawn, or
one whose store has gone — is not opened; the list stays where it was.

## Open a run's task from the side list — click its row, or one of its parts

With the switch on, a run is drawn in the conversation's side list as its own row,
`#N`, with its parts and their checks hanging under it. **Every one of those rows
looks exactly like any other task row**: the state mark (the spinner while it
works), the name, its `#id` at the far end — a part's id is the store's own, such
as `#k3x9qa` — and, under a row that is running, the command it is on and a
`4m · $0.02` line of how long it has run and what it has cost, each figure left
out when the store has not got it. The rows on the side list do not show tokens
or the model; the task's room does, where the store's spend has them. The parts hang in the tree's own connectors, one
row each, the finished ones included. Every one of those rows is a
door: click the run's row, or select it and press `enter`, and its room opens over the
conversation; click a part's row or a check's row and THAT task's room opens. It is the
room the tasks place opens and the run's tab opens: what the task was asked, its steps,
its notes, and the box that leaves a note. `esc` goes back to the conversation exactly as
you left it, with whatever you had typed still in the box.

The room can take a moment to arrive. From the press on, what you type belongs to the
room and never to the conversation: the keys are kept in order and land in the room's
box when it opens, `enter` included, unsent. `esc` in that moment withdraws the press.
If the row turns out to have no store page and its ordinary room opens instead, those
keys are dropped.

A room opened on a task that is queued or running follows it: it reads the task again
every three seconds, so a new step shows within that, and it stops reading when the task
has settled. A room on a task that has ended is read once, to open it. A step whose
command is many lines long is drawn as its first line and `…`; what ran is unchanged.

A row the store has no page for opens what it always opened, its room. That is every
task when the switch is off. A task of an earlier run keeps its page after a later run
has started.

## Can I still read a task from an earlier run?

Yes. Every run this conversation has made stays on the rail, oldest first. You
can open any task from an earlier run and read its description, notes, steps and
spend. An ended run is there to read, not to steer: a note, pause, resume, cancel,
amend or priority on one of its tasks answers `that task's run has ended` and changes
nothing. Only the run that is underway takes those. A run that has finished is ended
from that moment, even while it is still the newest run on the rail and nothing has
started after it: steering one of its tasks answers the same sentence.

## Does a new task pick up a run that did not finish? A new /task starts fresh

No. A new `/task`, or a new `codeaf do` in the same place, runs its own words in a run
of its own. It never carries on a run it did not start, and nothing runs an earlier
run's brief again.

Every way a run ends is written on the run's own task: done, your stop, a dollar or
time limit, `codeaf do --timeout`, or the run's own worker failing. Two endings leave
the run's own task unfinished, because nobody decided anything about the work: codeaf
itself closing while the run works (the engine ending it, or the program exiting) and
an interrupt of `codeaf do`. When codeaf closes, nothing is landed and nothing is
written on the run's record; every step it took is kept, and the run reads
`interrupted` from then on. An interrupted `codeaf do` still lands what it reached, as
it always has.

When a later task finds such a run unfinished in its store, it sets it aside first:
the run's own task and every part not yet ended are ended with the word `interrupted`,
and the store is kept beside the new one, readable with the earlier runs. Its rows
read `interrupted`, never `running`. Carrying an interrupted run on is not possible
from any surface today.

## I approved several tasks at once — are they one run? Why a task says it did not start

**Yes: tasks approved together are one run.** When the chat proposes several tasks
in one message and they are all approved at the same moment, the first one to start
opens the run and every other waits the moment that takes, then joins it as a child
of the run's own task, exactly as a task handed off a minute later would. A batch
never opens a second run beside the first and never sets the first run's plan
aside, and none of it starts on the older engine instead.

**A task whose run could not start says so, and nothing else starts.** If the run's
plan could not be opened or its copy could not be cut, the answer is
`task N did not start: <the reason>. Nothing is running for it and nothing was
started in its place; propose it again, or tell the person what stopped it.` A typed
`/task` answers the same sentence. It reads as a failure, never as `task N started`,
and the task is not quietly put on the older engine's tree. Only a build with no run
engine at all, or a conversation with nowhere to keep a plan, uses the older engine,
because there the run road was never there to take.

**A worker writes only its own run's plan.** A run's worker is bound to its run, not
only to where its plan was. If another run's plan is ever found in that place, the
worker's `plandb` refuses it: `the plan store at <path> is another run's (t-<its
task>), not this worker's run (t-<its own>), so nothing was read or written`.

## Typing into a run's row, and a row nothing drives any more

A message typed in the room of a run's task is left as a note on that task, and once
the store has it the room says `the worker reads a note at its next step`.

A run row that nothing drives any more, because its run is not the one this
conversation is driving or its plan holds no such task, answers a message with
`nothing is driving this task any more, so no worker can read a message; stop it to
clear the row`. It never answers `no task N in this session` while the side list
still draws it. *How do I stop a run?* says what clearing it does.

## Why is this task indented under that one?

The pane draws the run's **plan as a tree, not a flat list**. A task sits under the
task that requested it — the parent the worker wrote to the store — and a deeper
task sits under that, each joined to the row above it by the pane's own connector
(`├ `, `└ `). The shape of the run reads down the indentation.

A dependency never changes that family. `pending` means admitted and not started;
the row stays under the task that requested it and wears
`queued · waits: <that task>` to name the separate dependency.

A task's **page** shows its children under its steps the same way, in `under it`,
each drawn as the side list draws a task — mark, name, `#id`, the tree's `├─`/`└─`
— with the command it is on while its worker is on one. Notes, pause, cancel and the
rest of steering are unchanged by the tree.

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
in the tasks place (`/history`, `ctrl+.`, `alt+4`, and the roster raised over the frame), not
on the always-on column, which draws this conversation's own tree.

## What a run task's room shows while it runs

`enter` on a run's row opens the task's room, and while the task is running the room follows
its newest step: it re-reads the store every three seconds and stays stuck to the bottom,
the newest step in view, until you scroll up, which releases it. Scrolling back to the
bottom takes the follow up again without your pressing anything.

The step being run right now is the newest call in the transcript, drawn running. The
room's head says the task is working, how long it has run and what it has cost, as every
task room's head does. When the command ends the store clears the live step and the next
read draws it as an ordinary call, with what came back behind it.

## Why is a step missing, the step numbers skip, the cd at the front of a command is gone

**The steps a run's task shows are the work, cut from the commands as they ran.** Two
things are left out of a step's row, and nothing else is ever changed:

- a leading change into the run's own copy, which every command starts with and which says
  nothing about the task. A change into a folder further down is the work and is drawn
- any part of the command addressed only to the run's own record of the task, with whatever
  it is piped through. That is the run keeping its page up to date, not doing the task

A call that never ran has no row either. A worker that asks for several commands in one
answer has none of them run: each stays in the record with the answer it was given, and is
not drawn as a step, because nothing ran. A command the worker tried and was refused is
the one exception, and it is a line and not a step (see "A line under steps says refused").

Everything that is kept is drawn exactly as it was typed, spacing included. A command with
nothing left out is drawn whole. A part inside `$( )` or a bracketed group is never left out,
and neither is work that is piped into something else.

**A step with nothing of the work in it has no row.** The rows carry no numbers, as a task
room's rows carry none, so a page whose head says `12 steps` may draw fewer rows than that:
the rest are the run's own bookkeeping and calls that never ran. What the last of them said
is the task's result, which is in the notes above the steps.

## Why is there no output under a step, the dim line under a command is missing

**The dim line under a row is the first line of what came back, and it is only drawn when
it must be the row's own.** A command comes back with one answer for the whole line. When
the row left out a part addressed to the run's record, that part may have printed first,
and its words cannot be told from the work's, so the row has no dim line. A row that left
out only the change into the run's copy keeps it: that change prints nothing when it works,
and when it fails nothing after it runs. A row with nothing left out always keeps it. The
whole answer is on disk behind the row either way.

## A line under steps says refused — a command the worker tried and was not allowed to run

**`refused` and then a command, dim, with no number in front, is something the worker tried
that was not allowed.** The command is drawn as the worker typed it, cut the same way every
step's command is. Nothing ran, so the line is not a step: it has no number, nothing came
back to draw under it, and the rows around it keep the numbers they ran as, which is why the
numbers skip across it. The head's step count is the count of the record, so it includes it.

What was refused is the worker's attempt, by one of the limits a run's worker works inside:
reaching a remote, moving its copy onto work it did not do, writing outside the folders it
was given, or a call your permissions say no to. The worker is told why in full and carries
on with its next step. The page does not draw that answer, because it was written for the
worker. The whole answer stays in the task's recorded steps on disk.

An answer the run gives a worker about the FORM of its reply is different and draws nothing:
several commands in one answer, a call that could not be read, a tool that is not on its
belt. Nothing was tried there, so there is nothing to show.

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

- **note** — a note in your own voice on one task, which that task's worker is
  handed between its own steps. In a run task's room it is what the box
  sends: type in it and press `enter`, under the placeholder `a note for this
  task`. It is not a chat turn — the words go to the store and never to the
  conversation's model. "Does a note actually reach the worker" has its own
  section below.
- **pause** / **resume** — hold a task and everything under it out of the ready
  frontier without changing its rung, so running steps finish and nothing new in
  the subtree is launched; or release the hold. The key is `p`: a running row
  reads `p pause` and a held one `p resume`. The key is on the tasks place's
  rows only: in a task's room `p` is a letter in the box. A run cannot be paused
  as a whole, so its own row names no `p pause`.
- **cancel** — end a task, its descendants and the work hard-depending on it. The
  key is `x stop it` (the roster's own cancel key), on the row and in the room.
  In a room, `x` over an empty box raises the `Stop this task?` card first, for a
  part as for the run. On the run's own row and room that key ends the whole run
  and asks first; see "How do I stop a run?" below.
- **amend** — prepend text to a task's description, the way the CLI's `task amend
  --prepend` does, so the plan learns while it runs.
- **priority** — set a task's priority through the store's revision verb.

`x` and `p` are read only over an **empty box**: the moment there is a note to
type, a letter is a letter. A task that has ended, `done` or `incomplete`, is offered
neither: its row and its room name no `x stop it` and no `p pause`, because the store
would refuse both, **and neither key does anything there**. In an ended task's room
both are letters in the box; on an ended row in the list they are letters too. No
`Stop this task?` card is raised over a run that has already finished.

Two refusals are this layer's own, and they are the words the pane reads back:

- `no task <id> in this conversation`
- `that task belongs to another conversation`

A refusal the store itself answers travels back as the store wrote it, because the
store is the one that knows its own laws — a task that has ended cannot be
cancelled, and a revision is only for work that has not started. A hold asked of
the run's own task answers `a run is not held as a whole: hold one of its parts, or
stop it`.

## Does the chat know what is running while I talk to it

It does, while a run is live. Everything you send arrives with the run's rows in
front of it — a plain sentence, a message with pictures attached, and a draft you
marked standing alike: one line per task, the number you see on the side list,
its title, its state in the side list's own words (`queued`, `running`, `done`,
`stopped`, `incomplete`, `your call`), and the newest note left on it. You never
see that block — the conversation reads it, and your own sentence is what stays
on the screen and in the transcript.

- **It sees rows, never results.** The block carries no result, no steps and no
  output; those cost context and it can ask for them with `tasks` when it has a
  reason to. A run wider than eight rows shows eight and says how many more
  there are.
- **The live run comes first.** Rows of runs this conversation finished earlier
  come after the live run's, so history never pushes the work you are talking
  about out of the eight.
- **Only while something is open.** A conversation that has handed nothing out,
  or whose run has finished every row, gets none of this and pays nothing for it.
- **You are looking at the same picture.** The rows it reads are the rows on your
  side list, so if it says something about one of them you can check it.

What it is for is the next section: a sentence of yours can make work that is
already underway wrong, and it is the only thing in the room that can notice.

## I changed my mind and it kept going — skipping a part, dropping work you no longer want

Say it in your own words. "On reflection I do not want division in this package
at all" is enough: you do not have to name a task or a number. The conversation
already has the run's rows in front of your message, and it is told to act on
the row your sentence just made wrong **before** it answers you, and to leave the
rest alone.

- **What it does about it is its judgement, not a rule.** It may end the task,
  leave it a note with the fact it was missing, or tell you that work already
  landed and ask what you want instead. What it must not do is answer you and let
  the task carry on as if you had said nothing.
- **It can end a task and it can note one; it cannot rewrite one.** Changing what
  a task was asked for is the worker's own verb, not the conversation's, so "make
  it do X instead" comes out as an ending and a fresh task, or as a note the
  worker weighs. If you want the ended task's work kept, say so.
- **A task it ends is ended the way your own `x stop it` ends it.** Work halts
  where it stands, the branch is kept, nothing re-runs it and no check judges it.
  Ending one part of a run leaves the run going; ending the run's own row ends
  the whole run.

If it acted on the wrong row, say so — and if it kept going when you meant it to
drop something, the plainest fix is to name the row: "drop #2".

## say and forward on a row of a run, #2 or #2.1

The conversation reaches every row of a live run by the name the side list and
its own digest show — `#2` for a task it handed off, `#2.1` for a part the run
made for itself — with `tasks` and `stop` or `note`. Two more verbs of `tasks`
answer for those rows too, in words that say what happened:

- **`say`** on a row of a run is written onto the row as a note, because on a run
  that is what a line to the worker is. The answer opens "A row of the run takes
  `say` as a note, so your line went through `note`:", and the worker is handed
  it the way it is handed any note.
- **`forward`** on a row of a run is refused. It exists to move what a task is
  judged by, and nothing the conversation holds does that for a run's task. The
  refusal says so and names what does exist: `note` to put your words on the row
  as information, or `stop` and a fresh hand-off when the work itself is now
  wrong.

Neither ever answers that the row does not exist. A number no run of this
conversation holds still goes on to the ordinary task reader.

## Does a note actually reach the worker, when does it read it, does it have to ask for it

Yes, and it does not have to ask. The run looks for unread notes each time the
worker finishes a step, and hands them over at once, through the same door your
own typing into a running conversation takes. Before asking the model for its
next action, the worker waits for the run to record the completed step, apply its
limits, and hand over any notes. A slow record cannot let later actions race past
that boundary.

- **What it interrupts.** Nothing already running: the current step finishes
  first, and the next model request carries the note.
- **When it is missed.** If the worker's turn is ending in that same moment there
  is nothing to hand the note to, so it stays unread and the next step offers it
  again: an unread note is a note nobody has been told.
- **When it is not handed over at all.** A task that finishes before its next
  boundary never reads the note left on it — there is nobody left to tell — and
  the words stay on its page for you. A worker that has just been told it is
  repeating itself is handed nothing else at that boundary either; its note
  waits for the one after.
- **Once.** Each task is handed each note one time, across every worker it has: a
  worker launched again on the same task (a parent woken to integrate its children, a
  parked task woken) is not handed the notes an earlier worker of that task already
  had. A worker is never handed a note it wrote itself. Reading a note on the
  task's page does not use it up — you and the worker read the same notes, and
  what you opened is never a note the worker then missed.
- **Notes left before the task started** are handed over too, on its first step
  boundary. So a finding written onto a task that has not begun is waiting for
  its worker when it does.
- **Several at once** arrive together, up to five in one handover; any beyond
  that come at the next boundary.

**A note is not an order, and the worker is told so.** What it reads says the
note is something somebody knows, not a direction, and that its work order has
not changed. A note can never move what a task is judged by: asking for
something *different* is a revised assignment, not a note.

Three hands write notes — you, from the task's room; another worker in the run;
and the conversation itself — and each is named where the note is drawn: `the
person`, `task t-…`, or `you` when it was the conversation.

## Where do I see the notes on a run, why didn't the chat know about the note

Every note is in the task's room, after its steps, oldest first.

The conversation reads them too, and you can ask it: the run's listing puts the
newest note on each row after the row's state, as `note: …`, and asking about one
task prints that task's notes in full under `notes`, up to the last three. So
"what has anyone said about these tasks" is a question the chat can answer without
you opening a page.

Before this, a note was drawn on the task's page and nowhere else. A worker could
write down that another task's premise was wrong, and the conversation holding
what you actually asked for would list every row of the run and never learn it.

## How do I stop a run? Stop it did nothing and the task kept running, cancel the whole run

Press `x` over an empty box while the run's row is the one task row on the side list,
or open the run's task room and press `x` there over an empty box. Both raise the same
card, `Stop this task?`, with `stop it` and `keep going`. A digit moves the choice, `enter` takes it, and `esc` is
`keep going`. Nothing ends on one keystroke. Telling the chat "stop task 1" ends a run
the same way and asks nothing, because your sentence is the decision.

A stop ends the run now: every part still open is ended, what it was running is cut
off, and no further model call is made for it. The row reads `stopped`. A second stop
on a run that is already stopping answers that it is already stopping.

`x` on one PART's row in the tasks place ends that part only, at once and without a
card, and the rest of the run carries on. In a part's room `x` asks first, with the
same card, and ends that part only. A row nothing drives any more is cleared the same way:
the stop settles it as `stopped` and answers `stopped task N (<title>) — nothing was
driving it any more`. A run cannot be paused as a whole, so under the run's own
task no `p pause` is named.

Closing the window, `ctrl+c` and `/quit` do NOT stop a run: it carries on without the
window and its work lands by itself. Stop it first if you want it ended.

## What a stopped run keeps: its branch after you stop it, bring it in or drop it

**Nothing a stopped run made goes into your folder.** What it had made so far is
committed on the run's own branch, its copy of your folder is given back, and the
conversation and the run's page say where the work is and what to do with it:

```
stopped · its work so far is kept on <branch> and did not go into <folder> · merge that branch to bring it in, or delete it to drop it
```

A run stopped before it changed anything says `stopped · it had changed nothing` and
names no branch. The next `/task` after a stop starts a fresh run; it never picks the
stopped work back up, and the stopped run stays readable from the side list.

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
run's step cap, its wall, an errored turn, and one ending the worker reaches on
its own: the same command coming back with the same answer four times in a row,
which "Why did my task stop on its own?" explains.

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

A task that comes back **carries on its step numbers from where it stopped**: a page
that drew steps `1` to `5` before the wait draws the next one as `6`, never a second
`1`. The step cap is counted afresh each time the worker runs, so the numbers on the
page can pass the cap without the task having been stopped by it.

## Why did my task stop on its own?

A task stops itself for one reason of its own: **the same command came back
with the same answer six times in a row, and nothing changed between them.**
After the third identical look the run tells the worker once what it saw
and what it can
do: try something else; when waiting on something outside the plan that has
not changed yet, wait for it in one longer action that returns when it has
changed, and one action may run for up to 600 seconds; or park with
`plandb wait` when the plan names what it waits on. A worker that then does
something different is not stopped, and the count starts again; one that
keeps the same look three more times is stopped.

Whatever the command was, that is work that has stopped moving. The task ends
as `incomplete`, and its record closes with the reason `the same command came
back with the same answer 6 times in a row: the work was not moving`. The
steps above it on the task page show the command and what it got each time.

When the thing being waited on is outside the plan, a build, a deploy, a job
on another machine, the worker cannot park for it, because the plan does not
name it. Tell it what you know with a note from the task's page: a note on
the task counts as something changing, so the task is not stopped and the
count starts again. Or pause the task until the thing has changed.

Two kinds of worker are never stopped this way:

- **A task blocked on another task is parked, not stopped.** The plan itself
  names what the task is waiting on, a part not finished yet or a
  dependency, so the task waits exactly as if its worker had asked to, and
  comes back when that thing finishes.
- **A task whose repeated command keeps bringing back something different
  is left alone.** When each answer differs, or the task's own notes or one
  of its parts moved between the commands, the work is standing in front of
  something that changes, and only the step cap bounds it. What OTHER tasks
  in the plan do does not count: a task caught like this is stopped while
  the rest of the run carries on working.

## How does a task decide it is done?

Finishing is not reaching the end of the work — it is proving every requirement
of it. Before it runs `plandb done`, a worker walks each requirement sentence of
its own work order, and of the ask the run serves, one per line, and names
beside each the command or test that proved it in that run. A requirement with
no proof is not done: the worker proves it then, or reports it undone in its
result. Reaching the end of the steps is not the same as having met every
requirement in them, and speed is no permission to skip the walk.

## Costs and limits

- **The cost cap.** A run may spend what is left of the conversation's own **spend rail**
  — `/settings` → Spending → **per conversation** — which is off by default, or of
  `--max-cost` when codeaf was started with one and that is the smaller. What the run
  spends counts against it while it works. The run's width and its dollar ceiling are the
  conversation's own numbers, so a run costs what the conversation costs and runs as wide
  as the conversation may. A run started when that figure is already spent still gets
  one paid call — its first worker's — before the limit ends it.
- **Reaching the dollar limit ends every worker in flight**, whichever worker's
  spending crossed it: a live reading and a worker's final receipt end the rest alike.
  The run's own task is ended with `a limit you set stopped it`.
- **The time limit.** An elapsed-time limit on the session ends a run too: see
  "Does a time limit stop a running task?" on the page about starting codeaf.
- **The step cap.** A worker stops at **200** finished tool calls — the same
  figure a node worker carries. If the last call itself finished or parked the task
  in the store, that ending wins. Otherwise the cap stops the worker there without
  judging its work.
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
  the plan seat and no third model appears from the profile. A `/task` has no flags, so
  its check reads `CODEAF_CHECK_MODEL` alone. Without those pins,
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

## Does codeaf do commit my changes? It edits the folder in place and makes no commit of its own

`codeaf do` works in the directory you hand it with `-w` / `--dir` (the current
directory by default), **edited in place, on whatever branch is checked out there**.
codeaf makes no commit of its own. Its workers can commit their changes; when
the run ends, codeaf adds the bare `Assisted-by: CodeAF` and co-author lines to
those commits once. Any work still uncommitted is left for you to read, commit
or throw away.

Your own work is never touched by the run's accounting: an edit you had not
committed, or an untracked file such as a secrets file, is still yours after the
run, still uncommitted and still untracked. The files the run names — `files:` on
standard output, `artifacts` in `--json` — are the ones **this run** changed, read
by comparing the folder before and after: a file it wrote, a file of yours it edited
further, and anything it committed itself. A folder that is not a git repository
names no files, though the run's edits are still on disk.

The run's plan and worker transcripts stay in a private folder under
`~/.codeaf/runs/` (or `CODEAF_HOME/runs/`). The run leaves no `.codeaf/`
record in the directory it edits, and its record is never named among the
run's files.

## How much can a codeaf do run spend — --yes-spend, the plan price and today's limit

A `codeaf do` run has nobody watching, so **it stops at a price unless you said
otherwise**. Without `--yes-spend` it may spend up to the nearer of two figures:

- the **plan price** — `CODEAF_PLAN_CONSENT`, or the same row in `/settings`,
  **$100** out of the box — the point above which codeaf asks before it spends.
  Reaching it ends the run with exit **3**, `stop` `price`, and `blocked_on`
  saying the figure and to rerun with `--yes-spend`. `0` means never ask, and then
  this figure does not stop the run;
- what is left of **today's spending limit** (`CODEAF_DAILY_BUDGET`, `0` for no
  limit). Reaching it ends the run with exit **3** and `stop` `budget`; a day
  already spent starts nothing.

`--yes-spend`, or `CODEAF_PREAUTHORIZE_SPEND=1`, lets the run spend past both
without stopping. The older engine asked the plan-price question before it bought
anything; the run engine cannot price a run before its workers start, so the same
figure is a ceiling instead.

## codeaf do --db on the run engine, and where a run's store is

- **`--db` is refused**, with exit 1 and a sentence saying why: it names a store
  only the older engine works in, and a run holds its plan in a private folder
  under the state root's `runs/`. Drop the flag, or set `CODEAF_TASK_BELT=node`
  to run on the older engine, which takes it.
- **A done run removes its private folder** unless `--keep` or debug mode asked
  for it. A run that did not finish keeps it. The last line on the error stream
  names any kept folder: `record kept at <folder>`. It holds `plandb.db` and
  `tasks/` with the workers' trajectories and command transcripts.

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

The run counts a task as finished once its worker has come home, which can be a
moment after its row says so. A worker that ends its task in the middle of a
command comes home when that command ends, at most 600 seconds later, and only
then is its check added. The run waits for that, so every finished task is
checked before the run answers, whatever order the workers came home in. A check
the run cannot add ends the run as `incomplete`, even when the run's own task already
reads done; it is never skipped.

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

## How to turn it off

**This page describes the shipped default.** The harness is what a `/task` and a
`codeaf do` run on, on every machine, with nothing set. The environment variable
`CODEAF_TASK_BELT` is now the way *out* of it rather than the way in: set it to
`node`, `legacy` or `off` and the older engine serves every road again:

- a `/task` is a node of this session's own tree, not a run on the plan store;
- a task worker carries the conversation's own tools, not one shell;
- `codeaf do` is dispatched by the resident's reconciler, not the run engine.

Those three words are the only ones that turn it off. Any other value, including
an empty one, leaves you on the harness, so a typo cannot quietly move you to a
different engine.

The switch is read where the belt is composed, where a person's `/task` is
admitted, and where `codeaf do` chooses its road — and **with one of those three
words set, not one byte of any prompt, belt or landing moves from the older
road**.

Everything behind the switch is a seam. A build with no run engine linked, or a
conversation with nowhere to keep a plan, answers the older road — so a conversation
the run road cannot serve at all gets exactly the door it always had. A run road
that was there and failed does NOT fall back: the task answers `task N did not
start: <the reason>` and nothing is started on the older engine in its place.

# senior-dev

## What /senior-dev does — hand one large change to senior-dev, an autonomous coding agent

`/senior-dev <brief>` hands the whole brief to **senior-dev**, an autonomous coding agent
codeaf carries. At a shell the same program is `codeaf senior-dev <brief>`. It is built
into codeaf and runs only through it: there is nothing to install and no senior-dev of
its own to start.

It works alone in your folder itself, on a branch of its own when the folder is a git
repository, and your own branch never moves. It writes your brief down word for word, reads
the repository, keeps a checklist of what the brief asks for, pins a command that shows
the work passes, and edits until it believes the change is done. Then it **submits**:
the tree is frozen at that moment, so nothing it does afterwards can change what it hands
back. It then runs the project's own build and tests on the frozen tree, and if anything
moved after it submitted, the tree is put back to what it submitted.

It is for complex, multi-part coding work: fixing an issue in a mature codebase whose
cause spans files, a feature with its tests, a rewrite across a package, a migration.
codeaf hands work like that to it by itself, and uses it whenever you name it (the next
section). Its brief has to settle everything, because nobody will be asked anything.

## Will codeaf use senior-dev by itself — when does codeaf hand work to senior-dev, how do I make codeaf use senior-dev, stop it using senior-dev

**Yes, for the work it is for.** The chat's model is told to hand complex, multi-part
coding work to senior-dev, whole, rather than doing it in the conversation or giving it
to codeaf's own worker: fixing an issue in a mature codebase whose cause spans files, a
feature with its tests, a rewrite across a package, a migration. It proposes the task
with `via` naming senior-dev, and the card goes up like any proposal's, with its
countdown. A change you would make in a few steps it still makes itself.

**Naming it is enough.** Say senior-dev in your message, in any spelling: "fix issue 412
with senior-dev", "/senior-dev should take this", "senior dev". The model is told to use
it, and if it proposes the work without senior-dev anyway, codeaf turns that proposal
back once and tells it you named senior-dev. That holds even for a one-file fix, which
otherwise stays in the conversation.

**Saying not to is kept too.** "don't use senior-dev for this" names it, so the first
proposal is turned back the same way; the model proposes it again as it was, and the
second proposal for the same message passes.

**Typing `/senior-dev <brief>`** starts it at once, with your brief word for word and no
card. Work codeaf moves to a task on its own, because a reply ran long or looked like
work, goes to codeaf's own worker, never to senior-dev.

## Watching senior-dev work — open its task, what it is doing step by step, how long it has run, stop it

A senior-dev run is a task of the conversation that started it. Its row is on the side
list wearing `[senior-dev]` after its title, with the step it is in and what it has spent
so far under it, and a card in the conversation lands when it ends. Click the row or the
card, or follow a task link to it, and its task opens **inside the conversation's own
tab**: the tab strip stays on top, with the conversation's tab selected and `Home` beside
it. senior-dev gets no tab of its own.

The task shows **what senior-dev is doing**, action by action, each under the step of its
process it served — its brief, the workspace it set up, what it read and ran and changed,
its hand-in, the build and tests it ran itself, and how it finished — with the call to its
model in flight as the last line, `◐ thinking` and its seconds. The next section says what
each step means. The line over it pins the step, the spend of the run's ceiling, the
number of model calls and how long the run has been going — the same time the side list
and the landed card show, counted from the moment codeaf handed the work over. While the
task is open, its row on the side list leaves its clock out rather than show a time that
stopped when you clicked; the true time is back on the row the moment you leave.

**The raw calls are one key away.** `ctrl+y` turns the page to senior-dev's calls to its
model — what it sent, what the model answered, and which model it was — and `ctrl+y`
turns it back; the key row says `ctrl+y calls` or `ctrl+y actions`.

`esc`, a press on the conversation's tab, or a press on `Home` leaves it, and the run goes
on. `x` over an empty box, `/stop`, or `Stop` on that line asks `Stop this task?` first.
Nothing typed there reaches senior-dev: the box says `senior-dev reads no messages — say
it to main`, and `enter` says the same line and keeps your words in the box.

## What is senior-dev doing — the steps on senior-dev's page, what spec, explore, pin, checklist, implement, submit, verify mean

The word down the left of senior-dev's page, and on its row while it runs, is the step of
its own process an action served. senior-dev has no planner, reviewer or helper agent:
one model works through the middle steps in the order it chooses, so a step's word comes
back whenever it returns to that step.

- `setup` — it set up the folder it works in: `git`, or `no git history` for a plain
  folder, whose checkpoints it keeps outside it.
- `spec` — it wrote your brief down word for word as its spec, and read it back.
- `explore` — it read, searched and ran commands before changing any file.
- `pin` — it wrote down the one command that shows the work passes.
- `checklist` — it listed what the brief asks for, and ticked it off.
- `implement` — it changed files, and everything it read or ran after its first change.
- `submit` — it handed in its work: `handed in its work · 4 files · 5 of 5 ticked`, or
  `its hand-in was refused` and why. The work is frozen at that moment.
- `verify` — with no model, it ran the project's own build and tests itself, one line per
  command with `passes` or `fails · exit N`, then what they came to. It also checks the
  tree this way when its model stops without handing in.
- `finish` — what it did to the tree it leaves, the size of its change, and its ending.

Lines with no word of their own are senior-dev steering its model in the step already
under way, drawn quieter: `told its model what it found, and to finish and hand in (nudge
1)`, `time is short: gave its model one last turn to finish`, a dropped call retried, a
tool call written as text corrected — and `compacted its memory` and `switched to <model>`
with its reason.

## How do I tell a senior-dev task from a normal task — the [senior-dev] badge, [sd], what the brackets on a task mean

A task handed to senior-dev wears its name as a badge wherever a task is named:
`[senior-dev]`, bold in the accent colour, after the task's title. A normal task — one
`/task` starts, or one the chat hands to codeaf's own worker — wears no badge.

- **The side list** wears it after the title. When the list is too narrow for
  everything, the task's number (`#7`) goes first; then the badge shortens to its
  initials, `[sd]` — the narrower list a frame under 120 columns draws reads
  `⠋ rewrite the… [sd] #7` — and the title is cut last. Widened with `w`, the list has
  room for the whole badge and the number.
- **The card you answer** asks `wants to start a [senior-dev] task: <title>`, and the
  card's top line wears the badge beside the task's name.
- **The task's own page** wears it beside the title — a page onto another
  conversation's task too, with that task's own badge and never the one a task of the
  same number in this conversation wears.
- **The task strip**, the row of chips that stands in for the side list under 100
  columns, wears `[sd]`.
- **The `@` list, the tasks place and home** — its list of work, a landing under
  `needs you` and a line under `since you left` — wear `[senior-dev]`, or `[sd]` where
  the row is short of room. The title is cut before the badge, and a `since you left`
  line cuts what the work came to first: `rewrite the auth middleware [senior-dev] · it…`.
- **The chat's `tasks` tool** says `via senior-dev` on the row, so the chat can tell too.

The brackets are always drawn, so a terminal with no colour, the row of the task you
have open, and a screen reader all still show the badge. It is not a button: a press
anywhere on the row opens the task.

## How do I ask senior-dev for a change — writing the brief, what to put in it

The brief is everything senior-dev knows about what you want. It is saved as
`.senior-dev/spec.md` in the folder it works in exactly as you wrote it, and it is read
back from there whenever senior-dev summarises its own history, so the words you chose
are never paraphrased away.

Write it the way you would hand work to someone who cannot reach you:

- the files, packages or commands involved, by name;
- what done means, and how to check it (the test to run, the output to see);
- the constraints: what must not change, and the wrong answer to avoid.

In the chat, `/senior-dev` followed by the brief starts it as a task. At a shell, flags go
before the brief, and `--` ends them: `codeaf senior-dev run --variant high -- rename the
config loader`. Everything from the first word that is not a flag onwards is the brief,
so a flag written after the brief becomes part of it.

## Running senior-dev on a repository you have not cloned — a benchmark task, another project

senior-dev works in the folder it is handed and nowhere else, so it has to be handed the
repository the work belongs in.

In the chat, ask for the work and name the repository, and the commit if the work names
one. The model clones it first, into a new folder, at that commit, and hands senior-dev
that folder. A benchmark task works this way: senior-dev works in the project's own
repository and not in the benchmark's, so the benchmark's files, its reference solution
among them, are not in its folder.

At a shell, clone the repository yourself, then run `codeaf senior-dev` inside it or pass
the folder with `--dir`.

A brief that names the folder it works in is fine: senior-dev reads it as written. A
brief that tells senior-dev to make a checkout of its own somewhere else does not work:
its file tools refuse to write outside its folder, and what a shell command changes out
there is not part of the task.

## What senior-dev cannot do — it cannot ask you anything, no step cap, no Windows

**It cannot ask you anything.** Nobody is at its keyboard: a question its model tries to
ask is turned down inside the program, and after three it is told questions are not
available. Put everything it would stop and ask into the brief.

**It has no step cap.** It is held to the conversation's dollar and time ceilings instead,
and codeaf enforces both from outside whatever it does. On a service that reports no
prices the dollar ceiling cannot hold, and a time limit is the only bound (see the section
on services that report no prices).

**It reaches a model only through codeaf.** It holds no key and reads none; a
`senior-dev.json` in your folder that sets `apiKey`, `baseURL` or `providerRouting` is
refused by name, because codeaf decides which model service serves each call.

**It writes only inside its folder.** Its file tools (`write`, `edit`, `apply_patch`)
refuse any path outside the folder it was handed, including one reached through a link,
and say so to its model; it can still read files elsewhere. Its shell is not fenced the
same way, and nothing a shell command changes outside the folder is part of the task.

**It keeps its record in git**, on its own branch, unless it runs `--in-place`; codeaf
chooses that for a folder with no git history, from the chat and at a shell alike (see
the section on folders that are not a git repository).

**On Windows it is absent**: there is no `/senior-dev` and no `codeaf senior-dev`. Its
engine needs a Unix shell, process groups and file locks, so Windows builds leave it out
rather than carry something that fails every time.

## Can I run senior-dev in a folder that is not a git repo — a plain folder, no git, --in-place, operation not permitted, .Trash

Yes. The folder is read before senior-dev starts. **A folder with no git history — a
plain folder, or a repository with no first commit yet — is worked in as it is**, and
senior-dev is started with `--in-place`, from the chat and at a shell alike: it keeps its
checkpoints outside the folder and makes no commits. So is a folder inside a git
repository whose root is your home folder (a dotfiles repository): no branch is ever cut
there.

When it ends its changes are already in the folder. The task's page says `its work is in
<folder>, which has no git history, so nothing was committed` (or, under a repository at
your home folder, `its work is in <folder>; the git repository around it is at <repo>,
which holds your home folder, so codeaf cut no branch there and committed nothing`).

It works in your folder itself, so leave that folder alone while it runs: once it has
submitted, anything changed there is put back to what it submitted, and a file added
there is removed.

**A folder or file in it that senior-dev may not read is skipped**, not a reason to stop:
it is in none of its checkpoints, and nothing of it is changed or removed. senior-dev
needs no Full Disk Access; a folder macOS keeps to itself (`operation not permitted`) is
skipped like any other. It is never started on your home folder or a folder above it
(see the programs page): to check what it changed, it reads every file in the folder,
and your home folder is not one project.

A shell run used to stop at once there with `workspace is not a git repository:
<folder>; run with --in-place to work in a plain folder`. It no longer does: `--in-place`
is passed for you.

## Its notes — .senior-dev, its checklist, its session database, moved out when it ends

senior-dev keeps its own records in `.senior-dev/` in the folder it works in: the brief,
its checklist, the command it pinned, its session database and its whole conversation
with its model. **They are moved out of your folder when the run ends or you stop it**,
into the task's record folder beside `delegate-conversation.jsonl` (a shell run's record
folder at a shell), and the page adds `its notes (.senior-dev/) are kept in <path>`. So
they never end up on a branch, and the next run in that folder never reads the last
one's checklist as its own. A `.senior-dev/` already in the folder when the run began is
left where it is, and never ends up on a branch either.

## Where does senior-dev put its work — its own branch, checked out in your folder, not merged, not squashed

In a git repository, codeaf cuts a branch of its own for the run (`task/<title>-<id>`)
in your folder and checks it out there, and senior-dev works on it. senior-dev commits
every file it writes (`wip(write): <path>`, `wip(edit): <path>`) on that branch, which is
how it keeps a record to restore from; they stay there, and nothing squashes them.

When the run ends — finished or not, stopped, or crashed — codeaf commits whatever it
left uncommitted onto that branch, in one commit whose subject is the task's title and
whose body is senior-dev's own ending, and **leaves the branch checked out**, so the work
is in your folder when you look. Nothing is merged into your own branch. The task's page
and the conversation both say ``its work is on the branch <branch> in <folder>, N files,
and that branch is checked out there; your branch <yours> is as it was: `git -C '<folder>'
switch <yours>` goes back to it, and `git -C '<folder>' merge <branch>` from there brings
the work in``. Merge it when you are ready, or ask the chat to.

The ending keeps two witnesses apart: what senior-dev's model said it did when it
submitted (`senior-dev's model said: …`) and what senior-dev itself saw when it ran the
project's build and tests (`senior-dev observed: …`). Read the second for "did it work".

**A run you stop keeps its work the same way**: the stop says `its work so far stays on
its branch <branch>, checked out in <folder>` at once, and the page then says where it is
in the words above.

**A run that changed nothing leaves nothing**: your own branch is checked out again, its
empty branch is deleted, and the page says `it changed nothing, so <folder> is back on
your branch <yours> and its branch <branch> was deleted`.

## Does senior-dev change my branch — your branch never moves, going back, a HEAD it moved

No. Your branch (or, when your checkout was on no branch, the commit it was on) is
written down before senior-dev starts, and the run never writes to it, resets it or
merges into it. After the run your folder is on senior-dev's branch; `git -C '<folder>'
switch <yours>` goes back, and the page names the exact command. From no branch it
names `git -C '<folder>' switch --detach <commit>`.

senior-dev's shell can still run `git checkout`, and a brief that says "work on a new
branch" makes that likely. **So a brief need not ask for a branch: the work already has
one.** If HEAD is not on its branch when the run ends, nothing is touched, and the page
says where HEAD is: `senior-dev left <folder> on the branch <other> instead of its own
branch <branch>, so codeaf changed nothing there: nothing was committed and nothing was
switched; <branch> holds N files` (or `on no branch, at <commit>`). Look at that branch
before you commit anything there.

## senior-dev refused: changes that are not committed — a dirty checkout, uncommitted changes, a merge in progress

senior-dev works in your checkout itself, so it starts only on a clean one. **A repository
with changes that are not committed — modified, staged or untracked files — is refused
before anything starts**, nothing is switched and nothing is spent: `<folder> has changes
that are not committed (a.go, b.go, c.go and 2 more); commit or stash them, then ask
again`. senior-dev's own `.senior-dev/` does not count. A checkout in the middle of a
merge, a rebase, a cherry-pick or a revert is refused the same way: `<folder> is in the
middle of a merge; finish it or abort it, then ask again`.

In the chat the model is told this before you are shown a card, and can commit or stash
the changes itself if you ask it to; at a shell the run prints `error:` and the sentence,
and leaves.

## senior-dev refused: the folder is busy — one run per folder, another window, a shell run

One folder takes one senior-dev run at a time, from any conversation, any window or a
shell. A second is refused, naming the one working there: `<folder> is busy: senior-dev,
task 4 (Fix the parser), is working in it, and one folder takes one program run at a
time; ask again when that run has ended` (or `senior-dev, a run started at a shell`).
The hold goes with the codeaf holding it, however it ends, so a crash never leaves a
folder refused.

## What a senior-dev run costs — model calls, the dollar ceiling, which models

Every model call senior-dev makes goes through codeaf, which serves each run its own
model API. So every call is priced like one of codeaf's own, shows in the conversation's
total, its tokens and its call count, under `tasks` in `/cost`, and under the task on the
spend place, and is held to the run's dollar ceiling: **once the run's spend has
reached it, codeaf refuses every further call** before it is made, with
`the run's dollar ceiling of $5.00 is reached ($5.04 spent), so codeaf made no call`.
The call that crossed the ceiling was already made and paid for, so a run can end a little
over it. A refused call ends senior-dev's turn; it runs the project's build and tests on
the tree it has, and ends there, and the task says
`senior-dev reached the run's dollar ceiling of $5.00: …` with senior-dev's own words
after it. A run handed off after the conversation's dollar limit is already spent starts
nothing and makes no call: its row ends at once with `a dollar limit you set stopped it`.

The time ceiling is kept by senior-dev as well as by codeaf. It holds back the last part
of its time to land: two fifteenths of the run, at least 45 seconds, at most 12 minutes,
and never more than a quarter of it. When that window opens it gets one last turn to
submit.

**When none of your model services can serve the model it asks for**, codeaf answers the
call on the run's own work model — the one a task's own worker would use — and the raw
calls on the task page (`ctrl+y`) name the model that answered. When nothing here can serve
that model either, the conversation's own model may answer instead, and the page names
whichever model did. A dated build or a variant of the model it asked for, such as
`deepseek/deepseek-v4-pro-0731` or `qwen/qwen3.6-plus:free`, is that model and is not named
again; a sibling such as `openai/gpt-5.5-mini` answering for `openai/gpt-5.5` is a different
model and is named. Which models it asks for is the next section.

## senior-dev on a service that reports no prices — a local proxy, a Codex sign-in, the dollar ceiling does not hold, set a time limit

Some model services answer without saying what a call cost: most of the services you
connect in `/connect` besides the default router, such as a local proxy or runner, a
vendor's own API, or a plan you signed in to such as Codex. codeaf never guesses a price,
so each call
senior-dev makes through one is counted with its tokens and no dollars. The task page,
the rail and the spend place show no money for those calls, never `$0.00`, and a missing
price does not mean the service charged nothing.

**So the dollar ceiling cannot hold there.** A run whose calls report no price never
reaches its dollar ceiling, whatever it is set to, and senior-dev's own `--max-cost` adds
up the same missing figures. codeaf does not refuse such a run or estimate its cost.

**On such a service the bound that holds is a time limit.** Start codeaf with
`--max-hours`, or give a shell run `--max-hours`, before you hand the work off. With no
time limit, the run ends only when senior-dev finishes or you stop it.

## Why a stopped senior-dev run takes a moment to end — the price of the call it was in the middle of

When you stop a run, or codeaf ends it at its dollar ceiling, senior-dev is usually in
the middle of a model call. That call is still paid for, and the router prices a call cut
off like that by a receipt codeaf fetches afterwards, usually about twenty seconds later.
**The run is not over until that receipt is in**, for at most 70 seconds, so the task's
spend, the run's total and the conversation's `/cost` all include that call. A shell run
waits the same way before it prints its last line.

A receipt that never comes is kept as a call nobody could price, never as a free one
(the section `Was I charged for a reply that got cut off` says where those are counted).
A senior-dev call answered whole whose answer carried no usage block at all is asked
about the same way, including one codeaf then set aside because it was not usable text:
priced by its receipt, or kept as a call nobody could price. codeaf never guesses a figure
for either.

## Which models does senior-dev use — your crew, a model you ask for, its own list, --high

**Ask for a model and it works with that one.** Say which in the chat — "use senior-dev
with kimi-k2.6", or several: "with kimi-k2.6 and deepseek-v4-pro" — and senior-dev works
with exactly those, routing among them call by call when there are several; the card and
the task's first line name them. A name that fits more than one model is put to you to
settle. A model none of your connected services can serve is refused before the card, by
name, rather than swapped for another. A model senior-dev's model catalog does not know how to size cannot be used: the
run ends before its first call with `senior-dev cannot work with <model>: …`, and nothing
is spent. The models are fixed when the run starts; changing the crew later does not move
a run already working. `/senior-dev` typed with a brief uses your crew.

**Otherwise, from the chat it uses your crew.** codeaf hands senior-dev two of the conversation's
crew: the worker (hands) model is the one it works with, and the low model its history
summaries. Change the crew and the next run follows. The mastermind (brain) model is not
used: every call senior-dev makes is either its work or a history summary.
A crew model senior-dev's model catalog cannot size is left out, and its log says so;
if that leaves no working model, it uses its own list instead.

**Its own list** is six open models it routes among call by call, avoiding one for a
while after it fails: deepseek-v4-flash, deepseek-v4-pro, qwen3.6-plus, kimi-k2.6,
glm-5.1 and minimax-m2.7. A run with no crew set uses it, and so does a shell run.

**At a shell you choose**: `--high` replaces the list, `--low` sets the summaries' models,
and `--variant` sets the reasoning effort every call asks for.

## What a shell run prints at the end — how long senior-dev ran, what it cost, waiting for the last price

At a shell, `codeaf senior-dev` first says where it works (`senior-dev · working in
<folder>, on its own branch <branch>` in a repository), then prints each stage, step and
model call as it happens, then how the run ended, then where its work is (the sentence a
task's page says), then `the run's record is in` and the run's record folder, and last one
line with what it came to:

```
  277 model calls · $2.30 · 22m 51s
```

That is the calls, the dollars, and how long senior-dev's own process ran. A figure nobody
measured is left off, never written as a zero.

When ctrl-c or `--max-cost` stops the run in the middle of a model call, that call is still
paid for, and its price arrives by a receipt about twenty seconds later. The run waits for
it before those last lines, and says so on stderr:
`waiting up to 1m 10s for the price of 1 call that was cut short`. **A second ctrl-c leaves
at once** instead of waiting; a price still owed is then missing from the run's line and
from this machine's spending ledger.

Every call is written to this machine's spending ledger, filed as one piece of work named
after the run's record folder (such as `20260924-150405.000000`). That folder also keeps
`delegate-program.json`, with the instant senior-dev's process started and the instant it
ended.

## senior-dev's flags — run, --variant, --in-place, --high, --max-cost

`codeaf senior-dev <brief>` is `codeaf senior-dev run -- <brief>`. codeaf gives every
program it carries four flags:

- `--dir DIR` — the folder to work in (the current one by default; inside a git
  repository, the repository's root);
- `--max-cost USD` and `--max-hours H` — the ceilings;
- `--json` — the program's records on stdout instead of readable lines.

senior-dev's own flags on `run`:

- `--variant NAME` — reasoning effort sent with every call: `low`, `medium`, `high`,
  `xhigh`; unset leaves the model's own default;
- `--in-place` — work in a folder without git: no commits, and its checkpoints kept
  outside the folder. codeaf passes it itself for a folder with no git history;
- `--high`, `--low` — comma-separated models it routes among; `--low` (its history
  summaries) falls back to `--high`;
- `--frontier` — accepted, and changes nothing: no call senior-dev makes uses that tier;
- `--crew` — the models came from a conversation's crew: one its catalog cannot size is
  left out instead of failing the run. codeaf passes it with the crew's models.

`codeaf senior-dev help` describes it and its one command, `run`;
`codeaf senior-dev run --help` prints all of them, codeaf's four included.

## How long did senior-dev take — a run's time, the clock on its page, wall time

A senior-dev run is timed from the moment you handed it off — when its row first reads
`running`, after its folder is ready and its branch cut — to the moment senior-dev's own
process ended. Readying the folder before it, and committing what it left after it, are
not counted. A run whose senior-dev never started is timed to the moment the run ended.

Everything that shows the run's time shows that one span: the line under its page's title
(counting up from the hand-off while it runs, and stopped at senior-dev's exit once it has
ended, even before its last changes are committed), its row and card once it has ended, the note the
conversation is handed when it lands (`done · ran 22m 51s · …`), and the chat's `tasks`
tool (`#3 · <title> · done · ran 22m 51s · via senior-dev`, or `running for 3m` while it
goes) — so you can ask the chat how long it took. Each spells it the way the page does — `42s`, `22m 51s`,
`1h 7m` — except the landed card, which spells it `22m51s`.

The instants senior-dev's process started and ended are also kept in `delegate-program.json`
in the task's record folder, beside `delegate-stderr.log`.

**After a reopen.** A conversation closed and opened again still shows each run's time, how
it ended in senior-dev's own words (a `senior-dev did not finish: …` stays that sentence and
is not turned into a fault), which limit stopped it when one did, `stopped` when you stopped
it, and the branch its work is on.

**A run nothing is running any more.** If codeaf closed or crashed while senior-dev was
working, nothing is driving that run: its page reads `incomplete` rather than `running`,
its time stops at the last thing it did, and it offers no stop.

## Does another conversation or window see my senior-dev run — the @ list, other windows, the project's task list

Yes. A senior-dev run takes a row in the project's task list the moment it starts, saying
running, and a second row closes it when it ends, with its time, how it ended, the branch
its work was kept on and what it cost. So the `@` list, another conversation's `tasks`
tool, the conversation list's task counts and every other codeaf window on the project
see it, and a window that has the run's conversation open says it is being worked on. The
conversation that started the run lists it once, by the number its rail shows.

If codeaf went away while the run was working, its row is closed the next time that
conversation is opened, with the time the run had when it was last seen: it reads `codeaf
closed while senior-dev was running`, or the run's own ending when it had one. A run
senior-dev had finished but that codeaf closed under before the run was over reads
`incomplete — codeaf closed while this was still running`.

## Why did senior-dev stop — how a run ends, its log, crashed or stopped

A run ends in one of these ways, and the task's ending says which:

- `finished: …` — it submitted, and the words after say what the project's build and
  tests did on the frozen tree;
- `senior-dev did not finish: …` — it ended without submitting, or what it submitted fails
  the project's own build or tests. The task row shows this sentence as its reason; it is
  not drawn as a fault, and what it made is still on its branch;
- `senior-dev reached the run's dollar ceiling of $5.00: …` — codeaf refused a model call
  at the dollar ceiling; the words after are senior-dev's own ending;
- `senior-dev stopped on its own ceiling: …` — it stopped itself at the time ceiling;
- `senior-dev crashed: …` — the program itself broke, or could not start (no brief, a
  refused `senior-dev.json`);
- `stopped by the run: …` — you, or the run it belonged to, stopped it; what follows is
  what senior-dev said on its way out, usually `stopped before it finished`;
- `codeaf closed while senior-dev was running` — the codeaf holding its conversation
  stopped or crashed while it worked (see the next section);
- `senior-dev had ended; codeaf closed before it could say where its work is` —
  senior-dev had already exited, and codeaf stopped before it had committed what was left
  (see the next section).

When it ends without submitting, it still checks the tree it leaves. If the project's
tests cannot even start there, the tree is put back to the last state whose build and
tests could run, or to where it began.

## If codeaf quits while senior-dev works — closed, crashed, engine stopped, restarted mid-run

senior-dev ends with the engine holding its conversation. Leaving a hosted conversation's
window (closing it, `ctrl+c`, a closed terminal) only detaches: senior-dev keeps working.
When that engine is stopped (`codeaf engine --stop`, a signal) or crashes, the conversation
itself is closed, or a `--no-host` codeaf quits, the run is over: its page and side-list
row read `incomplete` with `codeaf closed while senior-dev was running` beside it, no
stage, nothing waiting on you, and no fault. If senior-dev had already exited, it reads
`senior-dev had ended; codeaf closed before it could say where its work is`.

**Its folder is finished by the next codeaf that finds the run**: the one that opens that
conversation, hands work off in it, or starts a run in that folder. What senior-dev left
uncommitted is committed on its branch, which stays checked out, its notes are moved out,
and the page adds where the work is, as a run that ended would say it.

**The run ends where it was last seen working**: senior-dev's exit, or else the end of its
last model call, its last charge, or its store's last change, whichever is latest. So its
time and spend do not count the hours codeaf was closed. An orderly close writes the ending
before senior-dev is stopped; after a crash the next codeaf that opens that conversation,
or hands work off in it, writes it.

**Nothing carries it on.** The next `/senior-dev` in that conversation starts a run of its
own, under its own task number, with its own brief and its own page. The old run's page
stays as the record of what it did.

## senior-dev's log — delegate-stderr.log, agent-summary, a shell run's record folder

Everything senior-dev said while it worked (each stage and what it knew at the time)
is kept in `delegate-stderr.log` in the task's record folder, and every stage, step and
ending it reported — what its page draws — in `delegate-actions.jsonl` beside it. Its `agent-summary` there
adds up each of its agents' calls, time and cost; the cost is the price codeaf's model
API told it for each call, not a catalog estimate, and a call nobody priced adds nothing. A run started at a shell has
no task, so its record — that log, its conversation with codeaf, its actions, its stages
and when it started and ended — is kept in a folder of its own under
`~/.codeaf/v3/carried/senior-dev/`, one per run.

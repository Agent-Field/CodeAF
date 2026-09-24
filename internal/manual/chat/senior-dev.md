# senior-dev

## What /senior-dev does — hand one large change to senior-dev, an autonomous coding agent

`/senior-dev <brief>` hands the whole brief to **senior-dev**, an autonomous coding agent
codeaf carries. At a shell the same program is `codeaf senior-dev <brief>`. It is built
into codeaf and runs only through it: there is nothing to install and no senior-dev of
its own to start.

It works alone in a copy of your folder. It writes your brief down word for word, reads
the repository, keeps a checklist of what the brief asks for, pins a command that shows
the work passes, and edits until it believes the change is done. Then it **submits**:
the tree is frozen at that moment, so nothing it does afterwards can change what it hands
back. It then runs the project's own build and tests on the frozen tree, and if anything
moved after it submitted, the tree is put back to what it submitted.

Use it for one change big enough to want an agent of its own for an hour, and specified
well enough that nobody will be asked anything: a rewrite across a package, a migration,
a feature with its tests. A change you would make in a few steps is not worth it.

## Watching senior-dev work — open its task, its conversation with codeaf, how long it has run, stop it

A senior-dev run is a task of the conversation that started it. Its row is on the side
list with the stage it is in and what it has spent so far, and a card in the conversation
lands when it ends. Click the row or the card, or follow a task link to it, and its task
opens **inside the conversation's own tab**: the tab strip stays on top, with the
conversation's tab selected and `Home` beside it. senior-dev gets no tab of its own.

The task shows senior-dev's conversation with codeaf: its brief, each model call with
what senior-dev sent and what the model answered, and the call in flight. The line over
it pins the stage, the spend of the run's ceiling, the number of calls and how long the
run has been going — the same time the side list and the landed card show, counted from
the moment codeaf handed the work over.

**The stage is said in plain words.** On the row and on that line senior-dev's stage
reads `starting`, `reading the brief`, `working`, `handing in its work`, `checking its
work` or `finishing` — never senior-dev's own names for its inner phases. The whole of its
work on the change, every model call and tool included, reads `working`; the build and
tests it runs at the end, and a last turn to leave its work in a state that stands, read
`checking its work`.

`esc`, a press on the conversation's tab, or a press on `Home` leaves it, and the run goes
on. `x` over an empty box, `/stop`, or `Stop` on that line asks `Stop this task?` first.
Nothing typed there reaches senior-dev: the box says `senior-dev reads no messages — say
it to main`, and `enter` says the same line and keeps your words in the box.

## How do I ask senior-dev for a change — writing the brief, what to put in it

The brief is everything senior-dev knows about what you want. It is saved as
`.senior-dev/spec.md` in its copy exactly as you wrote it, and it is read back from there
whenever senior-dev summarises its own history, so the words you chose are never
paraphrased away.

Write it the way you would hand work to someone who cannot reach you:

- the files, packages or commands involved, by name;
- what done means, and how to check it (the test to run, the output to see);
- the constraints: what must not change, and the wrong answer to avoid.

In the chat, `/senior-dev` followed by the brief starts it as a task. At a shell, flags go
before the brief, and `--` ends them: `codeaf senior-dev run --variant high -- rename the
config loader`. Everything from the first word that is not a flag onwards is the brief,
so a flag written after the brief becomes part of it.

## Running senior-dev on a repository you have not cloned — a benchmark task, another project

senior-dev works in a copy of the folder it is handed, and only what it changes in that
copy is kept, on the task's own branch. So it has to be handed the repository the work
belongs in.

In the chat, ask for the work and name the repository, and the commit if the work names
one. The model clones it first, into a new folder, onto a branch at that commit, and hands
senior-dev that folder. A benchmark task works this way: senior-dev gets a copy of the
project's own repository and not of the benchmark's, so the benchmark's files, its
reference solution among them, are not in its copy.

At a shell, clone the repository yourself, then run `codeaf senior-dev` inside it or pass
the folder with `--dir`.

A brief that names the folder you proposed is fine: codeaf rewrites that path to senior-dev's
copy before senior-dev reads it, so its commands run in the copy. The copy is of the whole
repository, so a subfolder you proposed becomes the same subfolder in the copy, and the
repository around it becomes the copy's root.

A brief that tells senior-dev to make a checkout of its own somewhere else does not work.
It has no copy of that folder, so nothing it does there lands: its file tools refuse to
write outside its copy, and what a shell command changes out there stays where it is.

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

**It writes only inside its copy.** Its file tools (`write`, `edit`, `apply_patch`)
refuse any path outside the folder it was handed, including one reached through a link,
and say so to its model; it can still read files elsewhere. Its shell is not fenced the
same way, and nothing a shell command changes outside the copy lands.

**It keeps its record in git**, unless it runs `--in-place`; from the chat, codeaf chooses
that for a folder with no git history (see the section on plain folders).

**On Windows it is absent**: there is no `/senior-dev` and no `codeaf senior-dev`. Its
engine needs a Unix shell, process groups and file locks, so Windows builds leave it out
rather than carry something that fails every time.

## senior-dev on a folder that is not a git repository — a plain folder, no git, --in-place

From the chat, codeaf reads the task's folder before it starts senior-dev. A repository
with at least one commit gets a copy, and the work lands on a branch of its own. **A folder with no git history —
a plain folder, or a repository with no commit yet — has nothing to copy from**, so
senior-dev works in that folder itself, and codeaf starts it with `--in-place`: it commits
nothing, and keeps its checkpoints outside the folder.

When it ends there is nothing to commit, because its changes are already in the folder.
The task's page says `its work is in <folder>, which has no git history, so nothing was
committed`. **Its own records are moved out of your folder** when the run ends or you
stop it: `.senior-dev/` (the brief, its checklist, the command it pinned, its session
database and its whole conversation with its model) goes into the task's record folder,
beside `delegate-conversation.jsonl`, and the page adds `its notes (.senior-dev/) are kept
in <path>`. A `.senior-dev/` already in the folder when the run began is left where it is.

It works in your folder itself, so leave that folder alone while it runs: once it has
submitted, anything changed there is put back to what it submitted, and a file added
there is removed.

At a shell, pass `--in-place` yourself. Without it senior-dev stops at once with
`workspace is not a git repository: <folder>; run with --in-place to work in a plain
folder`. A shell run moves nothing: delete its `.senior-dev/` when you are done with it.

To have its work isolated and left on a branch as one commit instead, make the folder a
repository with a first commit (`git init`, `git add -A`, `git commit`) before you ask.
Delete any `.senior-dev/` a shell run left there first, or `git add -A` commits its
database and its conversation.

## Where senior-dev's work lands — its own branch, not merged, one squashed commit

senior-dev commits every file it writes inside its copy (`wip(write): <path>`,
`wip(edit): <path>`), which is how it keeps a record to restore from. None of those
commits reaches your branch. When the run ends, they are squashed into **one commit**
whose subject is `task:` and the task's title, and whose body is senior-dev's own ending.

**That commit is left on the task's own branch in your repository, and nothing is merged
into your checkout.** The task's page says `its work is on the branch <branch> in
<folder>; nothing was merged into your checkout`, and the conversation is told
``its work is on the branch <branch> in <folder>, N files; nothing was merged into your
checkout, and `git -C '<folder>' merge <branch>` brings it in``. Merge it when you are
ready, or ask the chat to. A run can take an hour, and a merge at its end used to meet
whatever changed in your checkout meanwhile; now nothing can clash until you choose to
merge.

The ending keeps two witnesses apart: what senior-dev's model said it did when it
submitted (`senior-dev's model said: …`) and what senior-dev itself saw when it ran the
project's build and tests (`senior-dev observed: …`). Read the second for "did it work".

Its own notes live in `.senior-dev/` in the copy: the brief, its checklist, the command
it pinned and its session database. That folder is kept out of git, so it never lands.

**A run you stop keeps its work the same way.** What it had made by then, committed or
not, is squashed into one `task:` commit on the task's own branch, and the task says `its
work so far is kept on <branch> and did not go into <folder> · merge that branch to bring
it in, or delete it to drop it`, with the files it had changed.

When a run changed nothing, there is nothing to land and the task says so (`it had changed
nothing` for a run you stopped); its branch, which would hold nothing, is deleted rather
than left in your repository. On a folder with no git history nothing is committed at
all: the work is already in the folder.

## When it moved to another branch in its copy — "work on a new branch", my own branch, a detached HEAD

Its shell can run `git checkout` in its copy, and a brief that says "work on a new
branch" makes that likely. It changes nothing about where the work lands: when the run
ends, or you stop it, codeaf puts the copy back on the task's own branch without touching
its files, and squashes the finished tree onto it. The branch it had moved to is never
reset or committed on by codeaf, even when that is one of your own branches, so what it
left there stays.

The task's page says so beside the landing, in these words after the program's name:
`had moved its copy to the branch <branch>; its work was committed on <task branch>, and
any commit it made on <branch> is still on that branch`, or `had left its copy on no
branch; its work was committed on <task branch>`.

When the work was not built on where the copy started (it cut its own branch from
somewhere else), the squash also undoes whatever the copy's starting point had and its
work did not, and the page adds `its work was not built on the commit its copy started
from, so the commit on <task branch> may also undo changes that commit had; read its diff
before you merge it`.

When it had committed on the task's own branch before it moved, and the copy it left was
not built on those commits (it went back to the start to look at it, say), they are not
squashed away: the finished tree is committed on top of them, so every one stays on the
task's branch, and the page adds `the commits it had made on <task branch> are kept there,
under its finished work; that work was not built on them, so it may also undo their
changes; read its diff before you merge it`.

In either case the conversation is told too, after the merge command: `its work was not
built on everything that branch held, so the merge may also undo changes; read its diff
before you merge it`.

So a brief need not ask for a branch: codeaf already gives the work one.

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
call on the run's own work model — the one a task's own worker would use — and the
conversation on the task page names the model that answered. When nothing here can serve
that model either, the conversation's own model may answer instead, and the page names
whichever model did. Which models it asks for is the next section.

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
about the same way: priced by its receipt, or kept as a call nobody could price. codeaf
never guesses a figure for either.

## Which models does senior-dev use — your crew, its own list, --high

**From the chat it uses your crew.** codeaf hands senior-dev two of the conversation's
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

At a shell, `codeaf senior-dev` prints each stage, step and model call as it happens, then
how the run ended, then one line with what it came to:

```
  277 model calls · $2.30 · 22m 51s
```

That is the calls, the dollars, and how long senior-dev's own process ran. A figure nobody
measured is left off, never written as a zero.

When ctrl-c or `--max-cost` stops the run in the middle of a model call, that call is still
paid for, and its price arrives by a receipt about twenty seconds later. The run waits for
it before those last lines, and says so on stderr:
`waiting up to 1m 10s for the price of 1 call that was cut short`.

Every call is written to this machine's spending ledger, filed as one piece of work named
after the run's record folder (such as `20260924-150405.000000`). That folder also keeps
`delegate-program.json`, with the instant senior-dev's process started and the instant it
ended.

## senior-dev's flags — run, --variant, --in-place, --high, --max-cost

`codeaf senior-dev <brief>` is `codeaf senior-dev run -- <brief>`. codeaf gives every
program it carries four flags:

- `--dir DIR` — the folder to work in (the current one by default);
- `--max-cost USD` and `--max-hours H` — the ceilings;
- `--json` — the program's records on stdout instead of readable lines.

senior-dev's own flags on `run`:

- `--variant NAME` — reasoning effort sent with every call: `low`, `medium`, `high`,
  `xhigh`; unset leaves the model's own default;
- `--in-place` — work in a folder without git: no commits, and its checkpoints kept
  outside the folder;
- `--high`, `--low` — comma-separated models it routes among; `--low` (its history
  summaries) falls back to `--high`;
- `--frontier` — accepted, and changes nothing: no call senior-dev makes uses that tier;
- `--crew` — the models came from a conversation's crew: one its catalog cannot size is
  left out instead of failing the run. codeaf passes it with the crew's models.

`codeaf senior-dev help` describes it and its one command, `run`;
`codeaf senior-dev run --help` prints all of them, codeaf's four included.

## How long did senior-dev take — a run's time, the clock on its page, wall time

A senior-dev run is timed from the moment you handed it off — when its row first reads
`running`, after its copy has been made — to the moment senior-dev's own process ended.
Making the copy before it, and landing the work after it, are not counted. A run whose
senior-dev never started is timed to the moment the run ended.

Everything that shows the run's time shows that one span: the line under its page's title
(counting up from the hand-off while it runs, and stopped at senior-dev's exit once it has
ended, even before the work has landed), its row and card once it has landed, the note the
conversation is handed when it lands (`done · ran 22m 51s · …`), and the chat's `tasks`
tool (`#3 · <title> · done · ran 22m 51s`, or `running for 3m` while it goes) — so you can
ask the chat how long it took. Each spells it the way the page does — `42s`, `22m 51s`,
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
senior-dev had finished but whose work codeaf never brought in reads `incomplete — codeaf
closed while this was still running`.

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
  refused `senior-dev.json`, no git repository at a shell without `--in-place`);
- `stopped by the run: …` — you, or the run it belonged to, stopped it; what follows is
  what senior-dev said on its way out, usually `stopped before it finished`;
- `codeaf closed while senior-dev was running` — the codeaf holding its conversation
  stopped or crashed while it worked (see the next section);
- `senior-dev had ended; codeaf closed before its work was brought in` — senior-dev had
  already exited, and codeaf stopped before its work was landed (see the next section).

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
`senior-dev had ended; codeaf closed before its work was brought in`, and its work is
where senior-dev left it, not squashed.

**The run ends where it was last seen working**: senior-dev's exit, or else the end of its
last model call, its last charge, or its store's last change, whichever is latest. So its
time and spend do not count the hours codeaf was closed. An orderly close writes the ending
before senior-dev is stopped; after a crash the next codeaf that opens that conversation,
or hands work off in it, writes it.

**Nothing carries it on.** The next `/senior-dev` in that conversation starts a run of its
own, under its own task number, with its own brief and its own page. The old run's page
stays as the record of what it did.

Everything senior-dev said while it worked (each stage and what it knew at the time)
is kept in `delegate-stderr.log` in the task's record folder. Its `agent-summary` there
adds up each of its agents' calls, time and cost; the cost is the price codeaf's model
API told it for each call, not a catalog estimate, and a call nobody priced adds nothing. A run started at a shell has
no task, so its record — that log, its conversation with codeaf, its stages and when it
started and ended — is kept in a folder of its own under
`~/.codeaf/v3/carried/senior-dev/`, one per run.

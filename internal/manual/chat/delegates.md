# Programs codeaf carries

## What a program codeaf carries is — a delegate, another coding agent, an agent of its own for a whole task

codeaf carries programs of its own that take one whole coding task and do it alone, for as
long as an hour or more. People call them delegates. You hand one a task the way codeaf
hands a task to its own worker: it works in your folder itself, under this
conversation's dollar and time limits, shows on the rail while it runs, can be stopped,
and in a git repository leaves its work on a branch of its own, checked out, when it
ends.

Each one is **built into codeaf**. There is nothing to install and nothing to set up, and
none of them runs on its own outside codeaf. Each is a command in the chat, `/<name>
<brief>`, and a verb at a shell, `codeaf <name> <brief>`. The verbs are listed in
`codeaf --help`.

**It reaches a model only through codeaf.** codeaf serves each run its own model API.
Your key stays in codeaf and never reaches the program or any command it runs. Every
call the program makes goes through codeaf's own model road, so it is priced into your
spending, held to the run's dollar ceiling, and kept as one turn of its conversation with
its model. The run's task page, which opens inside the conversation's own tab like any
task's, shows the actions the program took, each under the step of its own process, and
`ctrl+y` turns it to those raw calls. When your services cannot serve the model the
program asks for, the run's own work model answers, and the raw calls name the model that
did.

**Every program's tasks wear its name as a badge**: `[<name>]` after the task's title on
the side list, the card, the task's page, the `@` list, the tasks place and home, and its
initials (`[sd]` for senior-dev) where a list is narrow. A task codeaf's own worker does
wears none, and a program added to codeaf later gets its own badge from its name.

This is different from a harness or a subharness, which are built out of codeaf's own
parts. A program codeaf carries has an engine of its own.

## How do I hand work to it — /<name> <brief>, codeaf <name> in a shell, via, delegate this to another agent

Type its name as a command, then the brief:

```
/<name> rewrite the auth middleware to use the new session store
```

That is `/task` with the worker chosen. A run starts at once in your folder, the turn goes
on, and the row appears on the rail.

The model can choose one as well, and reaches for one by itself (see *When codeaf hands
work to a program by itself*). `propose_task` takes `via` naming the program, and the
card you answer says which program the work is going to: it asks `wants to start a
[<name>] task: <title>`, and its top line wears the program's badge. The model is told
the programs your build carries, each in the program's own words: what it is for, what its
brief must say, and what it needs of its folder.

At a shell, `codeaf <name> <brief>` runs the same program in the folder you are in, or the
one `--dir` names. `--max-cost` and `--max-hours` set its ceilings, and `--json` prints its
records instead of readable lines. `codeaf <name> --help` lists its own commands and flags.
Its last line says what the run came to, such as `277 model calls · $2.30 · 22m 51s`: the
calls, the dollars and how long the program ran.

## When codeaf hands work to a program by itself — will it use one without being asked, naming one is enough, it did the work itself instead

The chat's model is told to hand the work a program is for to that program, whole, rather
than doing it itself or giving it to codeaf's own worker, even when it is one long job
with nothing to run beside it. Each program's own line says what it is for: senior-dev's
claims complex, multi-part coding work, such as fixing an issue in a mature codebase
whose cause spans files, a feature with its tests, a rewrite across a package, or a
migration. The model proposes that work with `via` naming the program, and its card goes
up like any proposal's.

**Naming the program is enough.** Say it in your message, by name or as its command
("fix issue 412 with senior-dev", "give this to /senior-dev", "senior dev should do
this"), and the model is told to use it. If it proposes the work without the program
anyway, codeaf turns that proposal back once, along with every other proposal without
it in the same reply:
``the person named senior-dev: if they want it to do this work, propose this again with `via: "senior-dev"`; if they asked for it not to be used, or did not mean the program, propose it again unchanged``.
A proposal the model makes after reading that passes as it is, so "don't use senior-dev
for this" is kept too. The next section says what else counts.

**What it does not do.** A reply codeaf moves to a task on its own, because it ran long or
looked like work, goes to codeaf's own worker and never to a program. A task never hands
its work to a program. `/<name> <brief>` starts the program at once, with no card.

## Naming a program in a small ask or a correction — fix this file with senior-dev, revert what senior-dev did, I typed a correction and it forgot senior-dev

**An ask for a program is never too small.** A one-file fix or a single read otherwise
stays in the conversation, but "fix this file with senior-dev" goes to senior-dev. For
that, your words have to ask for the program: its command (`/senior-dev`), its name
first in the message, or its name right after with, via, using, use, give, hand, to,
have, let, ask, get or want. A name in passing asks nothing: "fix senior-dev's typo in
this file" or "fix the line senior-dev changed in this file" stays here.

**A commit, an undo or a revert stays here, whatever it names.** "revert senior-dev's
commit", "commit senior-dev's changes" or "revert this commit with senior-dev" is done in
the conversation, and a proposal for it is refused: a program works on a branch of its
own and never moves yours, so it could not do it. `/senior-dev <brief>` still starts it.

**A correction does not undo the name.** Every message you type into one turn is read for
the program's name, not only the newest. Name senior-dev, then type "the failing test is
TestRetryUnderLoad" while it reads the code, and the name still holds for the rest of that
turn and for a turn a finished task or job wakes to answer it: a proposal without the
program is turned back as above, and "fix this file only" typed after the name still goes
to senior-dev. A correction that does not name the program earns no second turn-back. Your
next message that starts a turn of its own is read on its own.

## Which folder a program works in — a repository I have not cloned, it edited files outside its folder, a folder with no git

A program that edits code works in one folder itself, never a copy: the one the task
names as its `ground`, or this conversation's own folder when it names none (a typed
`/<name>` names none). Nothing else moves it — not `where`, not a path in the brief, not
where the conversation has been working — and the task's receipt names the folder.
Inside a git repository it is the repository's root. A `ground` that is not there yet is
made, empty, when the run starts, as long as the folder it would be made in is there.
Only what it changes there is part of the task. While it runs the folder is the
program's: codeaf's own file tools and tasks keep out of it (senior-dev's page says how).

**It is never handed your home folder**, or a folder above it: that is not a project. A
conversation opened in your home folder names the project's folder (making one first when
the work is new), and a hand-off that names none is refused with `<name> works in one
project's folder, and <folder> is your home folder; say which folder the work is in, as
ground`. `/<name>` typed there is refused the same way, and says to open codeaf in the
project's folder or to ask in the chat and say which folder.

So when the work belongs in a repository that is not on this machine (a benchmark task
that names a repository and a commit), the model clones it first, into a new folder, at
the commit the work names, and hands the program that folder, never a brief that sends
it to work in another folder.

A folder with no git history (a plain folder, a repository with no commit yet, or a
folder in a repository whose root is your home folder) is worked in as it is, and codeaf
tells the program so on the line it starts it with (senior-dev is given `--in-place`),
from the chat and at a shell. Nothing is committed: its changes are already in the
folder when it ends. senior-dev also reads this itself: it uses git only if git is
there, so it never ends for want of a repository.

At a shell, clone the repository yourself, then run `codeaf <name>` inside it, or name the
folder with `--dir`.

## What it cannot do — why it did not ask me, no questions, no step cap, no review round

**It cannot ask you anything.** Nobody is at its keyboard. Write the brief so that
everything it would stop and ask is already settled. The model is told the same thing when
it proposes one.

**It has no step cap.** It is held to this conversation's dollar and time limits. It is
given them when it starts, and codeaf enforces them from outside as well: once the run's
spend has reached the dollar ceiling, every further model call is refused before it is
made, and the task then says `<name> reached the run's dollar ceiling of $…`. The call
that crossed the ceiling was already paid for, so a run can end a little over it. On a
service that reports no prices (a local proxy, a vendor's own API, a plan you signed in
to) no call has a price to add up, so the dollar ceiling cannot hold: a time limit
(`--max-hours`) is the bound there.

**It has no review round.** codeaf's checker does not read its work afterwards. What the
program itself checked is reported in its result, kept apart from what its model claimed.

**It ends with the engine holding the conversation.** Leaving a hosted conversation's
window only detaches it. If that engine stops or crashes, the conversation is closed, or a
`--no-host` codeaf quits, the run ends with `codeaf closed while <name> was running` where
it was last seen working, or `<name> had ended; codeaf closed before it could say where its
work is` at the program's exit; the next codeaf to find the run says where its work is,
as it was left, and commits nothing. Nothing carries it on; the next hand-off starts a
run of its own.

## Why was the delegate refused — uncommitted changes, the folder is busy, it runs alone, no such program

**It needs a clean checkout.** In a repository with changes that are not committed
(modified, staged or untracked files), or a merge, rebase or cherry-pick half done, it is
refused before anything starts, and nothing is switched or spent: `<folder> has changes
that are not committed (<files>); commit or stash them, then ask again`, or `<folder> is
in the middle of a merge; finish it or abort it, then ask again`. The model reads this
before you are shown a card.

**One folder takes one program run at a time**, from any conversation, any window or a
shell: `<folder> is busy: <name>, task 4 (…), is working in it, and one folder takes one
program run at a time; ask again when that run has ended`. So do the folders inside it
and around it: `… is working in <held folder>, which holds it, …` (or `which is inside
it`).

**It runs alone.** While one is running, no other task can join it, and it cannot be
started under another run of this conversation: `work is already underway in <folder>;
<name> runs alone, so propose it again when that work has ended` (`in a copy of
<folder>` when the work underway is a task of codeaf's own). codeaf's own tasks run as the
conversation's run too, so a `/task` typed in a conversation while senior-dev is working
there is refused the same way, as a task that did not start; another conversation can
run one, on a different folder.

A name your build does not carry is refused with the ones it does:
`this codeaf carries no program called <name>; it carries …`.

## Where a delegate's work goes — its own branch, checked out in my folder, not merged into mine, not squashed, the wip commits, what it costs

In a git repository codeaf cuts the program a branch of its own (`task/<title>-<id>`) in
your folder and checks it out, and the program works there; its own commits (senior-dev's
`wip(edit): …`) stay on that branch, and nothing squashes them. When it ends, codeaf commits what
it left uncommitted onto that branch — the task's title, with the program's own account
of the ending as the body — and **leaves the branch checked out**, so the work is in your
folder. **Your own branch never moves**, and nothing is merged into it; if anything else
moved it during the run, the page says so instead of `as it was`. The task's page
and the conversation say ``its work is on the branch <branch> in <folder>, N files, and
that branch is checked out there; your branch <yours> is as it was: `git -C '<folder>'
switch <yours>` goes back to it, and `git -C '<folder>' merge <branch>` from there brings
the work in``. Ask the chat to merge it, or run that yourself, when you are ready.

A run you stop keeps its work the same way. A run that changed nothing leaves nothing:
your branch is checked out again and the empty branch is deleted (`it changed nothing, so
<folder> is back on your branch <yours> and its branch <branch> was deleted`). If the
program's own shell left the folder on another branch, codeaf commits and switches
nothing and says where it was left. Its own notes (senior-dev's `.senior-dev/`) are moved
out of the folder into the task's record folder, in any kind of folder.

A program that only answers works in your folder in place and changes nothing. Its answer
arrives in the conversation the way a task's landing does.

What it spent is in the conversation's total, in `/cost` and on the status line. Every
model call it made went through codeaf and is priced like one of codeaf's own. A run
stopped in the middle of a call is not over until that call's price has come in, for at
most 70 seconds, so the call it was cut in is in those figures too.

## Why is there no command for it — missing, not in this build, Windows, a hosted conversation

A program's command exists only in a build that carries it. On Windows codeaf carries
none: their engines need a Unix shell, process groups and file locks, so the commands are
absent there rather than failing every time.

Over `--host`, the programs are the far machine's build's. The rows come from that build,
and a run you start happens there, in that machine's folder, on a branch of its own.

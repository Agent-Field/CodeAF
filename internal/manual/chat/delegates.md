# Programs codeaf carries

## What a program codeaf carries is — a delegate, another coding agent, an agent of its own for a whole task, which programs there are

codeaf carries programs of its own that take one whole task and do it alone, for as
long as an hour or more. People call them delegates. There are two: **senior-dev**, a
coding agent for one large, well-specified change, and **sec**, which audits
the repository or its changes for security problems and changes nothing (its own page
says how). You hand one a task the way codeaf hands a task to its own worker. A program
that edits code, as senior-dev does, works in a private copy of its own in a git
repository and leaves its work on a branch of its own when it ends; in a folder with no
git history it works in the folder itself. One that answers, as sec does,
reads the folder and leaves its report. Each runs under this conversation's dollar and
time limits, shows on the rail while it runs, and can be stopped.

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

## Where a program's badge appears — and where it does not

**Every program's tasks wear its name as a badge**: `[<name>]` after the task's title on
the side list, the proposal card, the task's page, the `@` list, the sessions place and home, and its
initials (`[sd]` for senior-dev, `[s]` for sec) where a list is narrow. A task codeaf's own worker does
wears none, and a program added to codeaf later gets its own badge from its name.
The landed card's head shows the title and outcome without a program badge.

This is different from a harness or a subharness, which are built out of codeaf's own
parts. A program codeaf carries has an engine of its own.

## How do I hand work to it — /<name> <brief>, codeaf <name> in a shell, via, delegate this to another agent

Type its name as a command, then the brief:

```
/<name> rewrite the auth middleware to use the new session store
```

That starts a task with the program as its worker. A run starts at once in your folder,
the turn goes on, and the row appears on the rail.

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
migration; sec's claims a security audit of the whole repository or of the
changes on the branch. The model proposes that work with `via` naming the program, and
its card goes up like any proposal's.

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

A program that edits code works on one folder: the one the task names as its `ground`, or
this conversation's own folder when it names none (a typed `/<name>` names none). Nothing
else moves it — not `where`, not a path in the brief, not where the conversation has been
working — and the task's receipt names the folder. **Inside a git repository it works in a
private copy of the repository's root** — a git worktree in codeaf's cache folder, on a
branch of its own, removed when it ends — so your checkout is never touched and several runs can
work on one repository at once. **In a folder with no git history it works in the folder
itself**, and while it runs that folder is the program's: codeaf's own file tools and
tasks keep out of it (senior-dev's page says how). A `ground` that is not there yet is
made, empty, when the run starts, as long as the folder it would be made in is there.
Only what it changes there is part of the task.

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
it proposes one. A program that listens, as senior-dev does until it hands in, can still be
told something while it works; it never waits for a reply.

**It has no step cap.** Each program has finite dollar and wall-clock ceilings of its own
even when the conversation sets none — senior-dev's are $10 and three hours,
sec's $5 and two hours; `/budget conversation` can lower the dollar ceiling,
and shell flags set either ceiling directly.
An open chat's `/budget conversation` change binds its next proposal, run and turn
as soon as the setting receipt appears.
Before forwarding a call, codeaf reserves the larger estimate from the requested model
and its possible fallback seat when both have known prices, using input size and output
cap; if either price is unknown, it uses the unpriced bound. It refuses a call whose
estimate would cross the ceiling, and a call that would fit once calls already in flight have
ended (a program that makes many calls at once, as sec does) is asked to wait for
them instead and goes on when one ends. An
answer can cost more than its estimate. When a model has no known price, codeaf reserves
half the dollar ceiling and limits concurrent calls once half the recorded spend is used;
an unpriced service's actual charge cannot be measured here. The wall-clock ceiling still
ends the run.

**It has no review round.** codeaf's checker does not read its work afterwards. What the
program itself checked is reported in its result, kept apart from what its model claimed.

**It ends with the engine holding the conversation.** Leaving a hosted conversation's
window only detaches it. If that engine stops or crashes, the conversation is closed, or a
`--no-host` codeaf quits, the run ends with `codeaf closed while <name> was running` where
it was last seen working, or `<name> had ended; codeaf closed before it could say where its
work is` at the program's exit; the next codeaf to find the run finishes its copy —
commits what it left on its branch and removes the copy — or, in a folder with no git
history, says where its work is and commits nothing. Nothing carries it on; the next
hand-off starts a run of its own.

## Why was the delegate refused — uncommitted changes, the folder is busy, it runs alone, no such program

**Uncommitted changes do not refuse it.** In a repository its copy starts from your folder
as it stands: modified tracked files and staged additions form the first commit on its
branch. Untracked files are copied in as they are: any the run changes are committed as
its work, and the rest stay off its branch. Your files stay untouched; the receipt names
both. A merge,
rebase or cherry-pick half done is not carried, and the run starts from your last commit.

**Runs on one repository work side by side**, each in a copy of its own. **A folder with
no git history takes one program run at a time**, from any conversation, any window or a
shell: `<folder> is busy: <name>, task 4 (…), is working in it, and one folder takes one
program run at a time; ask again when that run has ended`. So do the folders inside it
and around it: `… is working in <held folder>, which holds it, …` (or `which is inside
it`).

**It runs alone in a conversation.** While one is running, no other task can join it, and
it cannot be started under another run of this conversation: `work is already underway in
a copy of <folder>; <name> runs alone in a conversation, so propose it again when that work
has ended` (`in <folder>` when the program works in a folder with no git history).
codeaf's own tasks run as the conversation's run too, so a `/task` typed in a conversation
while senior-dev is working there is refused the same way, as a task that did not start.
Another conversation, window or shell can run one beside it, on the same repository
too.

A name your build does not carry is refused with the ones it does:
`this codeaf carries no program called <name>; it carries …`.

## Where a delegate's work goes — its own branch, not checked out, not merged into mine, one commit, nothing to squash, no wip commits, what it costs

In a git repository codeaf cuts the program a branch of its own (`task/<title>-<id>`) from
the commit your checkout stands on, in a private copy of the repository, and the program
works there. senior-dev commits nothing as it goes — no `wip(edit): …` commits, and
nothing under its name — and its brief tells it not to commit or push even if yours asks.
When it ends — finished, stopped, crashed, or codeaf gone — codeaf commits what it left
uncommitted onto that branch in one commit (its usable message, followed by the run's
ending if it did not pass, or the task's title and ending as a fallback) and removes the copy, so **the branch is kept and
checked out nowhere**, even when the run changed nothing. **Your checkout is never
touched**: tracked edits and staged additions become its branch's first commit while staying
uncommitted in your folder. Untracked files it changed go on its branch; the rest stay off it
(senior-dev's page says how to bring the branch in).
The task's page and the conversation say ``its work is on the branch <branch> in <folder>,
N files; your checkout was not touched, and `git -C '<folder>' merge <branch>` brings it
in``. Ask the chat to merge it, or run that yourself, when you are ready; a branch published
under its own name is pushed instead (see senior-dev's "A run codeaf sends back").

If the program's own shell left its copy on another branch, codeaf commits nothing there
and keeps what was loose as a patch in the run's record folder. Its own notes
(senior-dev's `.senior-dev/`) are moved into the task's record folder, in any kind of
folder. In a folder with no git history its work is simply there, and nothing is
committed.

A program that only answers, as sec does, works in your folder in place and
changes nothing. Its answer arrives in the conversation the way a task's landing does, and
the chat tells you what it found and offers the next step it names; the files its answer
points at are in the task's record folder.

What it spent is in the conversation's total, in `/cost` and on the status line. Every
model call it made went through codeaf and is priced like one of codeaf's own. A run
stopped in the middle of a call is not over until that call's price has come in, for at
most 70 seconds, so the call it was cut in is in those figures too.

## When a program finished but the chat said nothing — done with no summary, no answer after it ended

When a program ends, its ending wakes a turn in the conversation, and that turn tells you
what it did or found. **The ending is never lost to a failed reply:** if that turn cannot
finish an answer because every model it is offered fails, the conversation says so in a
line of its own and gives the program's own account in full:
`<name> ended, but the chat could not finish its answer to it, so here is what <name> said:`.
It is written once per run. Ask again in your own words and the chat answers from it.

## Why is there no command for it — missing, not in this build, Windows, a hosted conversation

A program's command exists only in a build that carries it. On Windows codeaf carries
none: their engines need a Unix shell, process groups and file locks, so the commands are
absent there rather than failing every time. With no program available, the model is not
told about one and `propose_task` does not offer `via`.

Over `--host`, the programs are the far machine's build's. The rows come from that build,
and a run you start happens there, on that machine's folder, in a copy of its own on a branch
of its own.

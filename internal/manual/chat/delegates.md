# Programs codeaf carries

## What a program codeaf carries is — a delegate, another coding agent, an agent of its own for a whole task

codeaf carries programs of its own that take one whole coding task and do it alone, for as
long as an hour or more. People call them delegates. You hand one a task the way codeaf
hands a task to its own worker: it works in a copy of your folder, under this
conversation's dollar and time limits, shows on the rail while it runs, can be stopped,
and leaves its work on a branch of its own when it ends.

Each one is **built into codeaf**. There is nothing to install and nothing to set up, and
none of them runs on its own outside codeaf. Each is a command in the chat, `/<name>
<brief>`, and a verb at a shell, `codeaf <name> <brief>`. The verbs are listed in
`codeaf --help`.

**It reaches a model only through codeaf.** codeaf serves each run its own model API.
Your key stays in codeaf and never reaches the program or any command it runs. Every
call the program makes goes through codeaf's own model road, so it is priced into your
spending, held to the run's dollar ceiling, and shown as one turn of a conversation on the
run's task page, which opens inside the conversation's own tab like any task's. When your
services cannot serve the model the program asks for, the run's own work model answers,
and the page names the model that did.

This is different from a harness or a subharness, which are built out of codeaf's own
parts. A program codeaf carries has an engine of its own.

## How do I hand work to it — /<name> <brief>, codeaf <name> in a shell, via, delegate this to another agent

Type its name as a command, then the brief:

```
/<name> rewrite the auth middleware to use the new session store
```

That is `/task` with the worker chosen. A run starts at once in a copy of your folder, the
turn goes on, and the row appears on the rail.

The model can choose one as well. `propose_task` takes `via` naming the program, and the
card you answer says which program the work is going to. The model is told the programs
your build carries, each in the program's own words: what it is for, what its brief must
say, and what it needs of its folder.

At a shell, `codeaf <name> <brief>` runs the same program in the folder you are in, or the
one `--dir` names. `--max-cost` and `--max-hours` set its ceilings, and `--json` prints its
records instead of readable lines. `codeaf <name> --help` lists its own commands and flags.
Its last line says what the run came to, such as `277 model calls · $2.30 · 22m 51s`: the
calls, the dollars and how long the program ran.

## Which folder a program works in — a repository I have not cloned, it edited files outside its copy, a folder with no git

A program that edits code works in a copy of one folder: the one the task names as its
`ground`, or this conversation's own folder when it names none. Nothing else moves it —
not `where`, not a path in the brief, not where the conversation has been working — and
the task's receipt names the folder. **Only what it changes inside that copy is kept**, on
the task's own branch. Anything it changed anywhere else is not part of the task, and the
task's ending does not see it.

**It is never handed your home folder**, or a folder above it: that is not a project. A
conversation opened in your home folder names the project's folder (making one first when
the work is new), and a hand-off that names none is refused with `<name> works in one
project's folder, and <folder> is your home folder; say which folder the work is in, as
ground`. `/<name>` typed there is refused the same way, and says to open codeaf in the
project's folder or to ask in the chat and say which folder.

**The brief it reads names its copy.** Wherever the brief names the task's folder, codeaf
rewrites that path to the copy's before the program reads it, so it is never pointed at
your checkout. The copy is of the whole repository: a subfolder is rewritten to the same
subfolder in the copy, and the repository around it to the copy's root.

So when the work belongs in a repository that is not on this machine (a benchmark task
that names a repository and a commit, or a project you have not cloned), the model clones
it first, into a new folder, onto a branch at the commit the work names, and hands the
program that folder. It is told never to write a brief that sends the program to work in
another folder, because nothing the program did there could land.

A folder with no git history (a plain folder, or a repository with no commit yet) has
nothing to copy from, so the program works in that folder itself, and codeaf tells it so
on the line it starts it with (senior-dev is given `--in-place`). Nothing is committed: its
changes are already in the folder when it ends. Its own records (senior-dev's
`.senior-dev/`) are moved out of the folder into the task's record folder when it ends.

At a shell nobody does that for you: clone the repository, then run `codeaf <name>` inside
it, or name the folder with `--dir`.

## What it cannot do — why it did not ask me, no questions, no step cap, why it was refused

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

**It runs alone.** While one is running, no other task can join its copy, and it cannot be
started under another run. Both are refused with the folder that is busy:
`work is already underway in a copy of <folder>; <name> runs alone, so propose it again
when that work has ended`.

**It has no review round.** codeaf's checker does not read its work afterwards. What the
program itself checked is reported in its result, kept apart from what its model claimed.

**It ends with the engine holding the conversation.** Leaving a hosted conversation's
window only detaches it. If that engine stops or crashes, the conversation is closed, or a
`--no-host` codeaf quits, the run ends with `codeaf closed while <name> was running` where
it was last seen working, or `<name> had ended; codeaf closed before its work was brought
in` at the program's exit. Nothing carries it on; the next hand-off starts a run of its own.

A name your build does not carry is refused with the ones it does:
`this codeaf carries no program called <name>; it carries …`.

## Where its work goes — its own branch, not merged into mine, squashed into one commit, the wip commits, what it costs

A program that edits code works in a copy cut from your folder. When it ends, every commit
it made in that copy is squashed into **one commit**. The commit's subject is the task's
title, and its body is the program's own account of the ending.

**That commit stays on the task's own branch** (`task/<title>-<id>`) in your repository,
and **codeaf does not merge it into your checkout**. Your files and your branch are exactly
as you left them. The task's page says `its work is on the branch <branch> in <folder>;
nothing was merged into your checkout`, and the conversation is told the same with the
number of files and the command that brings it in, `git -C '<folder>' merge <branch>`.
Ask the chat to merge it, or run that yourself, when you are ready. Nothing can conflict
when the run ends, because the landing writes nothing of yours; a conflict only appears
when you merge.

A run you stop keeps its work the same way: what it had made by then is squashed into one
commit on the task's own branch, and the task says `its work so far is kept on <branch>
and did not go into <folder>`.

If the program switched branches in its copy, its work still lands on the task's own
branch, whether it ended or you stopped it, and the branch it had moved to (even one of
yours) is never reset or committed on by codeaf; the task's page names that branch.
Commits it had made on the task's own branch before it moved stay there, under its
finished work.

When there is nothing to land, it says `nothing to land: the run's working copy holds no
change` (`it had changed nothing` for a run you stopped), and the task's branch, which
would hold nothing, is deleted. A folder with no git history is the exception: the
program works in it directly.

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
and a run you start happens there, in a copy of that machine's folder.

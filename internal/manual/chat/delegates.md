# Programs codeaf carries

## What a program codeaf carries is — a delegate, another coding agent, an agent of its own for a whole task

codeaf carries programs of its own that take one whole coding task and do it alone, for as
long as an hour or more. People call them delegates. You hand one a task the way codeaf
hands a task to its own worker: it works in a copy of your folder, under this
conversation's dollar and time limits, shows on the rail while it runs, can be stopped,
and lands on your branch when it ends.

Each one is **built into codeaf**. There is nothing to install and nothing to set up, and
none of them runs on its own outside codeaf. Each is a command in the chat, `/<name>
<brief>`, and a verb at a shell, `codeaf <name> <brief>`. The verbs are listed in
`codeaf --help`.

**It reaches a model only through codeaf.** codeaf serves each run its own model API.
Your key stays in codeaf and never reaches the program or any command it runs. Every
call the program makes goes through codeaf's own model road, so it is priced into your
spending, held to the run's dollar ceiling, and shown as one turn of a conversation on the
run's task page. When your services cannot serve the model the program asks for, the
run's own work model answers, and the page names the model that did.

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

## Which folder a program works in — a repository I have not cloned, it edited files outside its copy

A program that edits code works in a copy of one folder: the one this conversation works
in, or the one the task names. **Only what it changes inside that copy lands.** Anything it
changed anywhere else is not part of the task, and the task's ending does not see it.

So when the work belongs in a repository that is not on this machine (a benchmark task
that names a repository and a commit, or a project you have not cloned), the model clones
it first, into a new folder, onto a branch at the commit the work names, and hands the
program that folder. It is told never to write a brief that sends the program to work in
another folder, because nothing the program did there could land.

At a shell nobody does that for you: clone the repository, then run `codeaf <name>` inside
it, or name the folder with `--dir`.

## What it cannot do — why it did not ask me, no questions, no step cap, why it was refused

**It cannot ask you anything.** Nobody is at its keyboard. Write the brief so that
everything it would stop and ask is already settled. The model is told the same thing when
it proposes one.

**It has no step cap.** It is held to this conversation's dollar and time limits. It is
given them when it starts, and codeaf enforces them from outside as well: a model call that
would cross the dollar ceiling is refused before it is made, and the task then says
`<name> reached the run's dollar ceiling of $…`.

**It runs alone.** While one is running, no other task can join its copy, and it cannot be
started under another run. Both are refused with the folder that is busy:
`work is already underway in a copy of <folder>; <name> runs alone, so propose it again
when that work has ended`.

**It has no review round.** codeaf's checker does not read its work afterwards. What the
program itself checked is reported in its result, kept apart from what its model claimed.

A name your build does not carry is refused with the ones it does:
`this codeaf carries no program called <name>; it carries …`.

## Where its work goes — squashed into one commit, landed on my branch, the wip commits, what it costs

A program that edits code works in a copy cut from your folder. When it ends, every commit
it made in that copy is squashed into **one commit**. The commit's subject is the task's
title, and its body is the program's own account of the ending. That commit is merged into
your folder the way every task's work comes home, so a program that commits after every
edit leaves no trail of bookkeeping commits on your branch. When there is nothing to land,
it says `nothing to land: the run's working copy holds no change`.

A program that only answers works in your folder in place and changes nothing. Its answer
arrives in the conversation the way a task's landing does.

What it spent is in the conversation's total, in `/cost` and on the status line. Every
model call it made went through codeaf and is priced like one of codeaf's own.

## Why is there no command for it — missing, not in this build, Windows, a hosted conversation

A program's command exists only in a build that carries it. On Windows codeaf carries
none: their engines need a Unix shell, process groups and file locks, so the commands are
absent there rather than failing every time.

Over `--host`, the programs are the far machine's build's. The rows come from that build,
and a run you start happens there, in a copy of that machine's folder.

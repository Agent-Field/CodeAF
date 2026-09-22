# Delegates

## What a delegate is — programs codeaf can hand a task to, outside agents, another coding agent

A **delegate** is an outside program on this machine that can do a whole coding task on
its own. codeaf hands it a task the way it hands one to its own worker: in a working copy
of your folder, under this conversation's dollar and time limits, shown on the rail while
it runs, stoppable, and landed on your branch when it ends. codeaf designs nothing about
the program and cannot see inside it; it starts it, reads what it says, stops it when a
limit is reached, and takes its result.

A delegate is not a harness and not a subharness. Those are programs built out of
codeaf's own parts and run inside this process; a delegate is somebody else's binary
running as a child process. `/harness` and `/subharness` list the first kind; `/delegate`
lists the second.

Each delegate is one manifest and one page under `~/.codeaf/delegates/`: `<name>.json`
says how to run the program, `<name>.md` says what it does. A delegate is found at launch,
so one added while codeaf is running appears the next time codeaf starts.

## How do I hand work to a delegate — /<name> <brief>, /delegate, via, "delegate this to another agent", the command for a delegate

Type the delegate's name as a command and the brief after it:

```
/<name> rewrite the auth middleware to use the new session store
```

where `<name>` is the word its manifest gives it — the command is generated from the
manifest, so it is spelled exactly as the manifest's `name`.

That is `/task` with the worker chosen. A run starts at once in a copy of your folder,
the turn goes on, and the row appears on the rail with the program's current phase as its
live step. `/delegate <name> <brief>` is the same door in long form.

The model can choose a delegate too: `propose_task` takes `via` naming one, and the card
you answer says which program the work is going to. It is told the names this machine has
and nothing else, so it cannot propose a delegate that is not here.

`/delegate` on its own lists every delegate on this machine, one line each: the command,
what it does, whether it lands its work on your branch or answers in the conversation, and
the program it resolved to. Under those, dimly, the manifests whose program is not on this
machine, and any manifest that was not added and why.

## What a delegate cannot do — why it did not ask me, no questions, no step cap, why a delegate was refused

**A delegate cannot ask you anything.** There is nobody at its keyboard: it runs
unattended, and a question it tried to ask is turned down inside the program. Write the
brief so that everything it would stop and ask is already settled. The model is told the
same thing when it proposes one.

**A delegate has no step cap.** It is held to this conversation's dollar and time limits,
which are handed to it on its command line and enforced by codeaf from outside as well. The
step count on its task page is what the program reported, not a limit.

**A delegate runs alone.** While a delegated run is going, no other task can join its copy,
and no delegate can be added under another run. Both are refused with the folder that is
busy: `work is already underway in a copy of <folder>; a delegate runs alone, so propose it
again when that work has ended`.

**A delegated run has no review round.** codeaf's checker does not read the program's work
afterwards; what the program itself checked is reported in its result, kept apart from
what its model claimed.

A name that is no delegate here is refused with the ones that are:
`no delegate is called <name>; the delegates here are …`. On a machine with none:
`no delegate is called <name>: this machine has no delegates (a manifest under
~/.codeaf/delegates adds one)`.

## Where a delegate's work goes — squashed into one commit, landed on my branch, the wip commits, what it costs

A delegate that lands a **tree** works in a copy cut from your folder. When it ends, every
commit it made in that copy is squashed into **one commit** whose subject is the task's
title and whose body is the program's own account of the ending, and that commit is merged
into your folder the way every task's work comes home. A program that commits after every
edit leaves no trail of bookkeeping commits on your branch. Nothing to
land is said as `nothing to land: the run's working copy holds no change`.

A delegate that lands **text** works in your folder in place and changes nothing; its
answer arrives in the conversation the way a task's landing does.

What it spent is in the conversation's total, in `/cost` and on the status line, folded in
as the program reports it. The spending page shows it under the delegate's name rather
than a model's, because the program's own calls did not go through codeaf.

## Why is there no command for my delegate — the delegate is missing, not on this machine, adding a delegate, the manifest was not added

A delegate's row exists only where its program does. `/delegate` says which of the
manifests under `~/.codeaf/delegates/` could not be added and why:

- `<name>: <program> is not on this machine` — the manifest is fine and the program is
  not on PATH. Install it and start codeaf again.
- `<name>: no manual page beside it — write <name>.md saying what /<name> does — not
  added` — every delegate ships the page the chat answers from.
- `<name>: its manual page does not say /<name> — not added` — the page exists and
  never names the command.
- `<name>: its name is already a command here — not added` — the name collides with a
  built-in command or alias.

Over `--host`, the delegates are the far machine's: `/delegate` lists what is installed
there, the rows are that machine's, and a delegate you start runs there, in a copy of that
machine's folder. A delegate installed only on this laptop is not offered in a hosted
conversation.

The contract a program has to meet to be a delegate is one page, `docs/DELEGATE-PROTOCOL.md`
in the codeaf repository: four kinds of line on its stdout, one terminal record, a clean stop
on SIGTERM, and its work left in the tree it was given.

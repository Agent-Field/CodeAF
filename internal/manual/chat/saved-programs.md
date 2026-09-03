# Saved programs

## What a subharness is — a saved program for a shape of work

A **subharness** is a saved program for a shape of work this project does again and again.
It is not a chat and not a prompt: it takes typed input, produces a typed answer, uses only
the tools it declared it uses, and runs inside bounds it declared.

The point of having one is that the good way of doing something stops depending on anybody
remembering it. A program that works out why a test is flaky does that the same way every
time, for the same money, and tells you the same shaped answer at the end.

Some are files on your machine under `~/.aforge/subharnesses`, or in the project you are
working in. Some are the pages a design wrote when you asked aforge to build you one, under
`~/.aforge/harnesses`. **They are one list and nothing in it says which is which** beyond
where each was found.

A **harness** is one of these too, reached by another door: `/harness` picks one and takes
your request as a sentence, `/subharness` lists it beside everything else and takes your
request as the one field on its card. *Saved shapes of work* is the page about designing
one.

## How aforge offers to run one — the "this looks like" card

When what you are asking for is what a saved program does, the chat can offer it. The
offer names the program and says why it matched, in one line:

```
this looks like flake-triage: the test name and the failure are both here
```

Under that line is the program's **intake card** — its form, with whatever this
conversation already answers already filled in.

**Nothing runs because the chat offered it.** The card is the consent, and answering it is
the only way anything starts. There is no countdown that says yes for you: a clock that
approved would be the program running because you were away from the keyboard, which is
exactly what this door is built not to do. If nothing is answered the card simply goes
away after a while and nothing has happened.

The verb the model uses for this is `propose_subharness`. It cannot run anything — all it
can do is raise the card.

**A weak match raises nothing at all.** The offer only appears when the program's own
trigger words, or its name, actually appear in what has been said. Anything less and the
chat says nothing, because a suggestion you have to swat away is worse than no suggestion.

## The intake card — the form that is already filled in

Every saved program declares what it takes. The card shows each field with what is in it:

- **Filled from the conversation** — the chat read it out of what you already said. You
  never get asked for something you have just told it.
- **The program's own default** — drawn dim. Nobody put it there; it is the program's
  answer for a question nobody answered.
- **Blank and required** — highlighted. These are the only things anybody asks you about.

You change what is wrong, fill what is missing, and confirm. Confirming is what launches
it. The same card opens whether the chat proposed the program or you picked it yourself.

On a card the chat raised, the last row is its two answers — `run it` and `no` — walked
with `←` / `→` and taken with `enter`, with `1` and `0` as the direct keys and `esc` as
another way of saying no. Under them is a line saying what the answer you are on will do.
*Subharnesses* is the page with the whole card and every key on it.

## What running one looks like — it is a task with a number

A run is a **task**, exactly like any other piece of work aforge hands out: a row on the
roster, a number you can say out loud, a room you can walk into and watch, and a ✕.

That is the whole reason it is worth confirming a card and then leaving. The run does not
hold the conversation — you can keep talking while it works — and what it produced arrives
back in the conversation when it lands.

Inside its room you see the program's own progress lines and every call it makes: each
model call, each tool call, each question. Its row on the roster carries whatever it last
said about itself, so you can tell what it is doing without going in.

A run does **not** have a branch or a working copy of its own. It works where the
conversation works, so
there is nothing to merge and nothing to check out afterwards.

## Stopping a run, and what is kept

Press ✕ on the row, or say so. The line you get back is:

```
stopping task 9 (run · flake-triage) — its journal is kept
```

It does not promise a branch, because a run does not have one. What is kept is the
account of everything the run did up to the moment you stopped it.

Stopping means **stop spending now**. It does not undo anything the run already did.

## When a program asks you something mid-run

A saved program can stop and ask you a question — "shall I open a pull request?" — and
wait for your answer. The question appears in the run's own room, and while it stands the
row says **awaiting your look**, so you find out even if you are doing something else.

There are three answers: what it asked for, and two others worth knowing about.

- Answer it, and the run carries on.
- **Take over.** The run ends where it stands with its journal complete, and what you
  typed becomes its answer for whoever picks the work up. This is for the moment you are
  watching something go slightly wrong and want to finish it by hand — you do not have to
  choose between killing it and waving it through.
- Stop it with the ✕, which is not an answer at all.

**With nobody watching — a headless run — a question is never quietly approved.** Each
question in the program declares what it means when there is nobody there: either a
specific answer to take, or nothing. Where it declared nothing, the run **stops
incomplete** and says which question it stopped at. An empty declaration is not a yes.

## When a run needed a closer look — handled the long way

Sometimes a program decides the work in front of it is not what it is for — a check it
makes about the material does not pass — or it breaks partway through. When that happens
the work is **not** abandoned and **nothing failed**. The job is handed to the ordinary
general worker with exactly the input the program was given, and you are told:

```
needed a closer look — handled it the long way
```

with the program's own reason after it where it gave one. You get your answer; it just was
not the fast path. This is the same in the chat and headless.

## When the long way is not taken — the ceiling holds

The general worker that does the job the long way has a shell, the files and the web, and
that belt cannot be narrowed. So the long way is only taken where the program's own tool
list already reaches a shell — because that is what you approved. A program approved to
read files and nothing else does **not** quietly become a shell agent in your workspace
because a check did not pass. Instead the run stops **incomplete** and says:

```
needed a closer look, and the long way would reach further than this program was allowed to — so nothing else was tried
```

again with the program's own reason after it. Nothing is lost: the run's journal is kept,
and you decide what happens next — run it again with different material, hand the job to
the chat yourself, or widen what the program may use. The ceiling you agreed to is never
widened without you.

## What a run costs

Everything a run spends — its model calls, and any tool call that costs model tokens —
lands on this session's bill. It shows up in `/cost`, against this conversation's
`per conversation` limit and in the status
line, exactly as work you did in the conversation does.

It does **not** count towards this conversation's context. A run has its own messages and
often its own model, so its tokens are billed to you without being counted as part of what
the chat is carrying.

A run whose provider reported no accounting at all adds nothing, and nothing is drawn for
it — that is "the provider did not say", which is not the same fact as free.

## Which tools a program may use

Every saved program declares the tools it uses, and that list is a **ceiling**. A program
that asks for anything else is refused, and the refusal names it. A program that declared
no tools at all can spend on model calls and nothing else.

Within that ceiling, a program's tool call goes through **exactly the same permission gate
your own tool calls go through**. If the policy would ask you before running something, it
asks — the question appears in the run's room — and while it stands the row says the run
needs you. Nothing a program does is quietly exempt.

Two refusals you may see, and they are different things:

- *"…is not one of the tools this program declared it uses"* — the program overreached.
- *"there is no … here"* — this build does not have that tool at all.

## Seeing what programs this machine has

The chat can list them with `list_subharnesses` — ask "what programs are saved here?" or
"what can you run?". Each row is a name, what it is for, where it came from, and — only
where there is history — a dim note about when it last ran and how it went. A program
nobody has run says nothing there rather than "never run".

The general worker is not on the list. It is what you get when you pick nothing, not
something you pick.

## Running one without the chat — aforge run subharness

```
aforge run subharness <name> --input <file.json>
aforge run subharness <name> --input -          # read the input from a pipe
```

Optional: `-w <dir>` for the directory to work in, `--model <slug>` for the work model,
`--journal <path>` to append every call the run makes to a file, one JSON object per line,
and `--json` for the one result object `aforge do` and `aforge exec` also print.

With no `--model` it runs on your crew's small-work class — the same crew `/crew` sets —
and it opens by saying which model it took and what chose it.

There is no task surface and no card here. What comes back:

| what happened | where it is said | exit code |
| --- | --- | --- |
| it finished | the report and the typed answer on stdout | 0 |
| the name is not a program here | the typo named on stderr, with what there is | 1 |
| it did not finish | the reason on stderr, in plain words | 2 |
| it needed a closer look | the long-way line on stderr, then the general worker's answer | 0 if that finished |

Those numbers are the same table `aforge do` and `aforge exec` leave on — 0 done, 1 it
could not be run at all, 2 it ran and did not finish, 3 a limit you set stopped it, 4 it
needed an answer and nobody was there. *Commands you type in a terminal* has the whole of
it, and the `--json` object beside it.

The reason a run did not finish is written for a person to read, and the word for it is
**incomplete** — the run happened and did not get to the end. Under `--json` that reason is
the `incomplete` field and the object says `"stop": "incomplete"`; the typed answer is in
`output`, whole.

## What is not available yet

- **Programs cannot be wired into a task graph.** A saved program runs on its own; it
  cannot yet take its input from another task's output or feed one, and one program cannot
  chain into another. That is a later phase, and nothing about it is half-built here.
- **There is no search over programs.** Matching is by a program's own trigger words and
  its name appearing in what was said, and nothing cleverer. A program whose words nobody
  used has to be asked for by name.
- **A program's own memory needs a store behind it.** A program can keep notes about its
  own domain across runs, and on a build with no bundle store wired those two doors simply
  are not there — the program is told so plainly rather than having its notes quietly
  dropped.

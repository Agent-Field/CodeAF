# The protocol, version 2 (internal)

*2026-09-23. What a program codeaf carries does, and what codeaf does for it. It
replaces the public, manifest-based v1 (`docs/DELEGATE-PROTOCOL.md`, kept on the
tag `delegate-manifest-v1`). The owner's plan is the "Built-in delegates plan"
doc; the Go types in `internal/delegate` are the specification, and this page
says what they mean. "Delegate" is a working title: a person only ever reads the
program's own name.*

## 1. What a program is

A value in the build's list, `internal/delegate/builtin`, of type
`delegate.Delegate`: a name (the chat command `/<name>` and the shell verb
`codeaf <name>`), a one-line summary, a guide, what it lands (`tree` or `text`),
its commands with their own flags, its default command, the flags that command
takes to work in a folder with no git history (`PlainFolder`), and the name of
its page in the chat's manual. There is nothing to install. A program not in the list does
not exist anywhere; on Windows the list is empty.

**The guide is the program describing itself to the model that hands it work:**
one paragraph of at most 400 bytes (`delegate.GuideMax`) saying what it is for,
and what its brief must hold. The conversation prints
it under the program's name, beside `propose_task`'s `via`, and says nothing about
the program of its own. It rides every request of every turn, which is why it is
short and why the manual page carries the rest.

**The program owns what is true of it; codeaf owns what is true of every
program.** That a program that edits files works in the task's folder itself, on
a branch of its own in a repository, and so the rule that it must be handed the
repository the work belongs in (cloned first when the machine lacks it, and never
briefed to work anywhere else) are codeaf's to say, once, beside the list; the
rule is printed only when a program that lands a tree is carried. That nobody can be asked
anything is `propose_task`'s own. A guide repeats none of it.

A program cannot run on its own. Its entry point is a `Command` whose body takes
a `delegate.Host`, and only codeaf makes one.

## 2. How it runs

Always as a child process of codeaf's own executable:

```
codeaf <name> <command> --json --dir <workspace> [--max-cost USD] [--max-hours H] [plain-folder flags] -- <brief>
```

**The folder is codeaf's to read, and the program is told what it found.** A
program that edits files works in the folder itself, never a copy
(`internal/session`'s `programfolder.go`, whose header is the contract): the
folder the proposal names, or the conversation's, or the shell's, snapped to its
repository's root. In a repository with a commit whose root is below the home
folder, codeaf writes the person's branch down, refuses a checkout with changes
that are not committed or a merge half done, and cuts the program's own branch
with `git switch -c task/<title>-<id>`; the program works there in its own git
mode. Anything else — no history, no commit, or a repository at the home folder —
is worked in as it is, and codeaf puts the program's own `PlainFolder` flags on
its line (senior-dev's is `--in-place`), because the program's own reading climbs
to any repository around the folder. senior-dev reads its folder too, and uses
git only where there is a work tree with a commit, so the flag is needed only
where git IS there and must not be used (the home folder's repository) — a
plain folder never ends it, whatever its line says. codeaf never learns a program's flag by
name, and a flag the default command does not take fails `Validate`, so the
build's own test catches it. One folder takes one program run at a time, held by
a file lock that dies with its process.

**The brief is handed over as written.** There is no copy for a path in it to be
rewritten into.

**A tree program's work stays on its branch, checked out.** When the run ends,
however it ends, codeaf commits what the program left uncommitted onto its branch
(the task's title, the ending as the body), moves the program's notes out of the
folder, and leaves the branch checked out; the person's branch never moves and
nothing is merged into it. The page says `its work is on the branch <branch> in
<folder>, N files, and that branch is checked out there; your branch <yours> is as
it was: …` with the commands that go back and bring the work in. A run that
changed nothing switches back and deletes its empty branch; a HEAD the program's
shell moved off its branch is left where it is and said.

**The crew rides the line.** A run a conversation starts carries its crew
(`delegate.Crew`: brain, hands, light — the mastermind, worker and low tiers,
effort taken off) in the program's own flags (`Delegate.CrewFlags`); a program
with none picks its own models. senior-dev's are `--crew --high <hands>
--frontier <brain> --low <light>`, and `--crew` makes it drop a model its
catalog cannot size, and route on its own list if the working seat is left
empty, rather than fail the run. `Validate` parses the flags with the default
command, as it does the plain-folder ones.

**A program's own ending names the row.** A terminal that is not `pass`
reaches the session typed (`run.ProgramEndedError` → `session.ProgramEnding`):
`fail` and `budget` end the row on `TaskEndingProgram`, whose reason is the
program's sentence (`senior-dev did not finish: …`) and which is not a fault;
`crashed` is `TaskEndingError`, the fault it is.

- **From the chat,** the engine's run (`internal/run`'s `DelegateWorker`) starts
  that line in the folder the proposal names (`propose_task`'s `ground`) or else
  the conversation's own.
- **From a shell,** `codeaf <name> <brief>` becomes the host: it serves the model
  API itself, readies its folder the same way, and starts the same child.

The two are told apart by the environment. A child of a host has
`CODEAF_MODEL_API` and `CODEAF_MODEL_TOKEN`; a person's shell has neither.

The child's environment is the parent's with every provider key and model
redirection codeaf knows of removed (`delegate.ChildEnv`). The program passes its
environment on to every command its model runs, so a key left there would be one
any model-written shell line could print.

## 3. The model API — the only road to a model

For each run codeaf serves an OpenAI-style chat-completions API at
`CODEAF_MODEL_API` (a base URL), opened by the bearer token in
`CODEAF_MODEL_TOKEN` and by nothing else. It lives in `internal/provider`, the
one package codeaf's funnel law lets spell a model route. Every call:

1. is refused before it is made when the run's dollar ceiling is reached, with
   HTTP 402 (a status senior-dev does not retry). A run a refusal ended is
   reported as `<name> reached the run's dollar ceiling of $X: …`, whatever
   status the program itself wrote, and ends on the run's cost limit;
2. goes through codeaf's own model funnel, with its router, retries, caching and
   billing, on the model the program asked for when one of the person's
   services can serve it, and otherwise on the run's work seat, which the turn
   names in `Served` (`modelapi.Resolve`; a call is never refused only because
   the machine does not know the id);
3. is answered in the OpenRouter shape, `usage.cost` included, streamed with
   keepalives while a long call is thinking, or as one body when it was not
   streamed (`response_format` carried);
4. is banked to the task's spend and the spending ledger, and written to the
   run's conversation log.

The token dies with the run, so a grandchild that outlives its parent can no
longer spend. A call's thread is its `prompt_cache_key`, or its
`x-session-affinity` header when the body carries no key; reasoning effort rides
codeaf's own effort ladder.

A shell run (`codeaf <name> …`) has no task folder, so its record — the
conversation log, the program record and the program's stderr — goes to
`~/.codeaf/v3/carried/<name>/<when>/`, one folder per run. Its child is started
with the person's own line plus `--json`, so a command other than the default
and the command's own flags survive.

## 4. The records — stdout, one JSON object per line

| record | when | fields |
| --- | --- | --- |
| `hello` | first | `protocol` (2), `delegate`, `stages` (the whole list, in order) |
| `stage` | on every phase change | `stage`, `status`, and optionally `data`: a JSON object of at most 1024 bytes (`delegate.StageDataCap`) |
| `step` | once per finished action | `command` (one line, 200 bytes at most), `observation` (2048 bytes at most), and optionally `tool` (the tool's name), `step` (the program's own id for the part of its process the action served), `exit` (a command's exit code, only for an action that ran one), and `added` and `removed` (the lines an action that changed a file added and removed, only when the program counted them) |
| `terminal` | last, exactly once, on every path | `status` (`pass`, `fail`, `budget-exhausted`, `crashed`), `message`, `data`: `reason`, `claim`, `observed`, `deliverable`, and anything else |

Any other line is ignored. There is no `spend` record: the model API meters
every call as it is made, so money has one source of truth and it is not the
program's word.

**The optional fields are additive, and they are version 2.** The version moves
only when a record changes meaning (`delegate.ProtocolVersion`); a field a
reader does not know is ignored like any other, so a reader that predates
`data`, `tool`, `step` and `exit` reads the same records without them, and a
program that sends none of them is read exactly as before. They are read
forgivingly: a `tool`, `step`, `exit`, `added` or `removed` of another JSON shape is left off and
the step kept, and `data` that is not an object, or is past the cap, is left
off and the stage kept.

A stage's `data` is a small, curated copy of what the program already knows
about the phase — senior-dev's is an attempt, a retry count, its checklist's
counts, the hand-in's size, what its own check found, the model it moved to —
for a page to say in words; the program's whole account stays on its stderr.
senior-dev's step ids are `brief`, `explore`, `pin`, `checklist`, `implement`,
`submit` and `verify` (`internal/seniordev/app`'s `Steps`); the last is the
project's own build and tests, which senior-dev runs itself with no model after
the hand-in and when it checks the tree mid-run, each command one step.

A `hello` carrying another protocol number means the engine outlived a rebuild
and started the new binary as its child. The run is stopped before it spends,
with the reason `codeaf was rebuilt while this conversation was open …; restart
codeaf to run <name>`.

## 5. Stop

SIGTERM to the process group, a 15-second grace, then SIGKILL. On SIGTERM the
program stops starting new work, writes its terminal, and exits. A body that
returns without writing a terminal gets one written for it (`delegate.RunChild`).

A host that dies without a word — killed, or taken by a closed terminal's
hangup, which never reaches a child in a process group of its own — sends no
SIGTERM. The child looks for its parent once a second and, when the process that
started it is no longer its parent, stops exactly as a SIGTERM would stop it, and
is ended outright if it is still at work when the grace has passed
(`delegate.RunChild`'s `watchHost`). A shell run's host itself treats SIGHUP as
its first ctrl-c.

## 6. The conversation log and the action log

`delegate-conversation.jsonl` in the task's record folder, one `delegate.Turn`
per model call: the thread, the model asked for and the one that answered, what
the program sent that the thread's previous call had not, the reply and the tool
calls, tokens and cost, and codeaf's refusal or the model's failure. A call is
written when it starts and again when it ends, and a reader keeps the later
record, so the task page shows the call in flight.

`delegate-actions.jsonl` beside it, one `delegate.Action` per record the program
wrote — `stage`, `step`, and its ending (`end`: the terminal's status and
message) — each stamped `at` with the moment codeaf received it, because a
program's own clock is not trusted and the page merges this log with the
conversation log, whose times are codeaf's too. The run's worker writes it and
so does a shell run, into its own record folder; it is capped as the turns are.

**The page draws actions, not the dialogue.** A program's own vocabulary
(`Delegate.Present`, a reader told every line of the log in order) turns each
line into what a person reads under the step of the program's process it
served (`delegate.Shown`); the page merges those with what only the calls know
— a compaction, a change of the model answering, a refused or failed call — by
time, and keeps the dialogue of the raw calls one key away. The live step names
the step the program is in: the word the program's reader gives the latest
record that named one, and its stage's word before any has.

## 7. What a program may not do

- Ask a person anything. Nobody is at its keyboard. (Later: a tool codeaf runs
  inside the model API.)
- Read stdin.
- Reach a model any way but the model API.
- Write anything on stdout that is not a record on its own line.
- For `tree`: touch files outside its workspace, or leave anything in it that is
  not its work (its own state git-excluded). senior-dev enforces the first for its
  file tools: `write`, `edit` and `apply_patch` refuse a path outside the
  workspace, links resolved (`tool.RegistryOptions.ConfineWrites`), while reads
  stay open. Its shell is not fenced; its prompt says that nothing a shell
  command changes outside the workspace comes back.

## 8. Built in now for later programs

pr-af and sec-af, looked at on 2026-09-23, would need: plain structured calls
with `response_format`, many conversations at once (kept apart by thread),
grandchildren inheriting the API's address and token, quiet stretches of up to
30 minutes, and text landings with attachments. The first four are in v2 from
the start; attachments come with the first text program.

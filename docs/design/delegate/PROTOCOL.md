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
program.** The copy a program that edits files works in, the rule that only that
copy lands, and so the rule that it must be handed the repository the work
belongs in (cloned first when the machine lacks it, and never briefed to work
anywhere else) are codeaf's to say, once, beside the list; the rule is printed
only when a program that lands a tree is carried. That nobody can be asked
anything is `propose_task`'s own. A guide repeats none of it.

A program cannot run on its own. Its entry point is a `Command` whose body takes
a `delegate.Host`, and only codeaf makes one.

## 2. How it runs

Always as a child process of codeaf's own executable:

```
codeaf <name> <command> --json --dir <workspace> [--max-cost USD] [--max-hours H] [plain-folder flags] -- <brief>
```

**The folder is codeaf's to read, and the program is told what it found.** A
repository with a commit gets a working copy cut from it. A folder with no git
history (a plain folder, or a repository with no commit) has nothing to cut
from, so the program works in the folder itself and codeaf puts the program's
own `PlainFolder` flags on its line (senior-dev's is `--in-place`); its landing
commits nothing, because the work is already there, and the run's page says so.
codeaf never learns a program's flag by name, and a flag the default command
does not take fails `Validate`, so the build's own test catches it.

- **From the chat,** the engine's run (`internal/run`'s `DelegateWorker`) starts
  that line in the run's working copy, which is cut from the folder the proposal
  names (`propose_task`'s `ground`) or else the conversation's own.
- **From a shell,** `codeaf <name> <brief>` becomes the host: it serves the model
  API itself and starts the same child.

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
| `stage` | on every phase change | `stage`, `status` |
| `step` | once per finished action | `command` (one line, 200 bytes at most), `observation` (2048 bytes at most) |
| `terminal` | last, exactly once, on every path | `status` (`pass`, `fail`, `budget-exhausted`, `crashed`), `message`, `data`: `reason`, `claim`, `observed`, `deliverable`, and anything else |

Any other line is ignored. There is no `spend` record: the model API meters
every call as it is made, so money has one source of truth and it is not the
program's word.

A `hello` carrying another protocol number means the engine outlived a rebuild
and started the new binary as its child. The run is stopped before it spends,
with the reason `codeaf was rebuilt while this conversation was open …; restart
codeaf to run <name>`.

## 5. Stop

SIGTERM to the process group, a 15-second grace, then SIGKILL. On SIGTERM the
program stops starting new work, writes its terminal, and exits. A body that
returns without writing a terminal gets one written for it (`delegate.RunChild`).

## 6. The conversation log

`delegate-conversation.jsonl` in the task's record folder, one `delegate.Turn`
per model call: the thread, the model asked for and the one that answered, what
the program sent that the thread's previous call had not, the reply and the tool
calls, tokens and cost, and codeaf's refusal or the model's failure. A call is
written when it starts and again when it ends, and a reader keeps the later
record, so the task page shows the call in flight. The page draws the turns as
the conversation between the program and codeaf.

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

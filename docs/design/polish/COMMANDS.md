# The command surface

What `aforge` is called from a terminal, and why. This is a design: where today's
surface is incoherent it says what it should be instead, and where it is already
right it says so, so that nobody "fixes" it.

The audit of the code against this page is `audit-commands.md` beside it.

---

## 1. The one sentence

`aforge --help` opens today with **"aforge — build and revise task graphs"**
(`cmd/aforge/main.go:233`). That describes a static pipeline that is now four of
twenty-three verbs, and it opens the product's front door with two words a person
outside this repository has no use for.

The line should be what the manual already says aforge is
(`internal/manual/chat/starting-aforge.md`):

    aforge — an agent you talk to, and hand work to when you walk away

Everything below follows from that sentence: there is a surface you sit in front
of, there is work you hand over, and there is the record of what happened.

## 2. The verbs

Five families. **`aforge --help` prints them under these five headings, in this
order** — most-reached-for first, not alphabetical, because a flat list of
twenty-three lines is a list nobody reads to the end (today's is exactly that,
`docs/design/polish/frames/cmd-help.txt`).

### Talk to it

| Verb | What a developer means by it |
| --- | --- |
| `aforge` | open the conversation this directory was last having |
| `aforge chat` | the same thing, with flags |
| `aforge resume` | pick an earlier conversation by name and open it |

Interactive. Draws a screen, reads keys, never useful in a pipe. The one
exception is `aforge chat --once "…"`, which is the chat surface answering one
message with no screen — the door a shell script uses when it wants *this
conversation's* agent rather than a fresh one.

### Hand it work

| Verb | What a developer means by it |
| --- | --- |
| `aforge do "<task>"` | do this, however many steps it takes, and tell me when it stands |
| `aforge exec "<prompt>"` | one worker, one pass, no planning — the door a harness calls |
| `aforge run <program> --input <file>` | run one saved program on typed input |

Headless. Nobody is watching, the answer goes to stdout, the exit code is the
verdict. **These three are the whole of "make it work for me"**, and the
difference between them is *how much thinking aforge does before it starts*:
`do` plans and can split the job, `exec` does not plan at all, `run` follows a
plan somebody already saved. That sentence belongs in `--help` under the
heading, because the names alone do not carry it and never will.

`aforge run <program>` is today `aforge run subharness <name>`. See §7.

### Look at what happened

| Verb | What a developer means by it |
| --- | --- |
| `aforge tasks` | what has run here, newest first |
| `aforge tasks <id>` | what one piece of work actually did — turns, tools, arguments, how it ended |
| `aforge spend` | what today cost, and on what |
| `aforge logs` | every model call: what was asked, who answered, what came back |
| `aforge models` | the models this machine will use, and what they have been measured at |
| `aforge doctor` | is this install healthy, and where is its state |
| `aforge manual [page\|question]` | aforge's own account of itself, printed whole |
| `aforge version` | which build this is |

**Read-only, and none of them spends a cent or needs a key.** That is a promise
worth writing down, because it is why they are safe to put in a shell prompt, a
CI step or a bug report. `aforge models` today reaches the network for the model
catalog (`cmd/aforge/models.go:41`) — it must degrade to the cached listing
rather than hang, but it still spends nothing.

`aforge tasks` and `aforge spend` are today `aforge why <node-id>` and
`aforge why self` (`cmd/aforge/why.go`). See §7.

### Housekeeping

| Verb | What a developer means by it |
| --- | --- |
| `aforge cache` / `aforge cache clean` | how much disk the shared build cache holds, and free it |
| `aforge rebuild` | throw away everything derived and replay the journal |
| `aforge serve` | be reachable from your other devices without ssh |
| `aforge devices` / `aforge devices revoke <name>` | who may open a conversation here, and take it back |

Changes state on disk or on the network. **Every destructive one asks first, and
`--yes` is the one word that skips the question.**

### The graph pipeline

| Verb | What a developer means by it |
| --- | --- |
| `aforge graph plan "<goal>"` | write a plan to a file without running it |
| `aforge graph show <file>` | read that file back as a table |
| `aforge graph revise <file> "<what happened>"` | change the plan in the light of what happened |
| `aforge graph run <file>` | run the plan exactly as written |

The by-hand pipeline: a plan you can read, edit and diff. It is a real feature
and it is not what most people want, which is why it is last and why it is under
one noun. Today these are four top-level verbs (`plan`, `show`, `revise`, `run`)
sitting in the same list as `chat` and `do`, and one of them collides with a
different command entirely (§7).

### Not printed

`aforge engine` and `aforge tick` are machinery: one is what ssh starts on the
far machine, one is what the OS timer runs. Neither draws anything or reads
anything. **They stay out of `--help`, out of the typo suggester, and in the
manual under the pages that explain remote work and standing orders** — which is
where somebody debugging would actually look. This is already the arrangement
and it is right (`cmd/aforge/main.go:135`, `:179`).

## 3. The nouns

A verb acts on one of these, and these are the only words that appear in an
argument position or an id:

| Noun | What it is | Where it is named |
| --- | --- | --- |
| a **conversation** | one thread you talk in; a file on disk | `--session <path>`, `aforge resume` |
| a **task** | one piece of work you handed over | `aforge do "<task>"`, `aforge tasks <id>` |
| a **program** | a saved shape of work, run on typed input | `aforge run <program>` |
| a **plan** | a task graph in a file | `aforge graph …` |
| a **call** | one request to one model | `aforge logs --call <id>` |
| a **device** / a **machine** | something paired with this one | `aforge devices`, `--host`, `--at` |
| the **cache** | shared toolchain downloads and builds | `aforge cache` |
| the **store** | this machine's journal and derived tables | `--db <path>` |

Words that are **not** nouns in this vocabulary and must not appear in anything a
person reads: *leaf*, *node*, *spine*, *seat*, *sheet*, *charter*, *lane*,
*brain*, *errand*, *settled*, *contract*, *ensemble*, *panel*, *rail*, *verdict*.
Several of them are in `--help` today; the audit lists each one.

Two of these deserve their replacement stated once, because they recur:

- a **leaf** is *a step*. `--turns` is a backstop *per step*.
- a **rail** is *a limit*. `--yes-spend` raises *today's spending limit*.
- a **lane** is the exception. `internal/manual/chat/lanes.md` is a person-facing
  page using *lane* to mean the provider route that answered, and `aforge logs`
  prints exactly that. **Keep `lane` in that one meaning and nowhere else.** It
  is established product vocabulary with a page of its own, not machinery
  leaking; renaming it would cost a manual page and buy nothing.

## 4. The flags, as one vocabulary

**One spelling per concept, everywhere, or the flag does not exist.** A developer
who learned `--dir` on `do` must be able to type it on `exec` without checking.

| Concept | The spelling | Carried by | Default, and why |
| --- | --- | --- | --- |
| the directory to work in | `--dir` | `do`, `exec`, `run`, `graph plan`, `graph run`, `serve`, `engine` | the current directory. Never a freshly created subdirectory: a one-shot that files its work somewhere nobody looks has not done the work. |
| where to write the result | `--out` | `do`, `exec`, `graph plan`, `graph run`, `graph revise` | stdout |
| a machine-readable answer on stdout | `--json` | `do`, `exec`, `logs`, `graph plan`, `graph revise` | off |
| which model does the work | `--model <slug>` | every headless verb, `chat` | flag › `AFORGE_MODEL` › the crew › the built-in default |
| which model plans | `--plan-model <slug>` | `do`, `graph plan`, `graph run`, `graph revise` | the work model |
| how hard it thinks | `--reasoning off\|low\|medium\|high` | `chat`, `do`, `exec` | off |
| a wall in wall-clock time | `--timeout <duration>` | `do`, `exec`, `graph run`, `wake` | `do` 15m; `exec` scales from the token wall |
| a wall in tokens | `--token-budget <n>` | `exec`, `graph run` | 150000 |
| a wall in model turns | `--max-turns <n>` | `exec`, `graph run` | 200 |
| a wall in dollars | `--max-cost <n>` | `chat --yolo` | none |
| a wall in hours | `--max-hours <n>` | `chat --yolo` | none |
| how many steps at once | `--parallel <n>` | `graph run` | 32 |
| do not ask before deleting | `--yes` | `cache clean`, `rebuild` | off |
| do not ask before spending more | `--yes-spend` | `do`, `graph run` | off |
| the store to work in | `--db <path>` | `doctor`, `notebook`, `services`, `tasks`, `spend`, `rebuild`, `wake`, `competence`, `do` | this machine's chat store |
| keep the whole record of the run | `--debug` | `chat`, `do`, `exec` | off |
| run it on another machine | `--host <host[:path]>` over ssh, `--at <name[:path]>` over the relay | `chat` | here |
| run every tool without asking | `--yolo` | `chat` | off |

Rules that fall out of that table, and are the point of it:

1. **`--budget` is a word about money in this product** — `AFORGE_DAILY_BUDGET`,
   `/budget` in the chat, `--max-cost`. So the token wall is `--token-budget` and
   never `--budget`. Today `exec --budget 150000` and `run --budget 150000` are
   tokens, which reads as $150,000.
2. **A duration flag takes a duration.** `--timeout 15m`, `--timeout 2h`,
   `--timeout 900` (bare seconds, one release). One parser, shared, on every
   verb that has a wall. `--max-seconds` on `wake` is the same concept spelled a
   third way and becomes `--timeout`.
3. **Single letters are shorthands, never the only spelling.** `-w`, `-o`, `-j`
   keep working forever as hidden aliases; `--dir`, `--out`, `--parallel` are
   what is printed. Today `-w` has no long form at all, so per-command help
   prints it as `--w` (`cmd/aforge/usage.go:110`) while the table prints `-w`,
   and a reader cannot tell which is real.
4. **A flag that does nothing is not accepted.** `exec --plan-model` is
   documented as ignored (`cmd/aforge/exec.go:49`). Accepting a flag in order to
   ignore it teaches a harness author a wrong thing quietly. It is removed, and
   `exec` says so if it is passed.
5. **A tri-state is words, not magic integers.** `--ensemble 0|-1|N` becomes
   `--passes auto|off|<n>`.
6. **A boolean that defaults on gets a negative spelling.** `graph run
   --contracts` defaults true and can only be turned off as `--contracts=false`,
   which no other flag in the binary needs. It becomes `--no-method`, and the
   thing it turns off is called *a working method*, not *a contract*.
7. **A flag is documented by what it does, not by what it sets.** `--context-fill`
   and `--completion-reserve` explain themselves today by naming the environment
   variable they write. That is the implementation.
8. **A default that has a constant is interpolated from it.** The usage table
   already does this for the dollar figures (`cmd/aforge/main.go:228`) and it is
   right. The per-flag sentences do not: `--completion-reserve` says
   "(default 65536)" in prose while its `DefValue` is 0.

`--model` and `--plan-model` are the model of how this should read: one help
string, defined once (`modelFlagHelp`), naming the whole precedence ladder, used
on six doors. **Nothing about them needs changing.**

## 5. The rules the surface keeps

### stdout and stderr

**stdout carries the answer and nothing else.** The deliverable, the JSON, the
rows, the table. Anything a person reads *about* the run — the models line,
progress, a warning, a question, the path a record was kept at — goes to stderr.

`aforge do` already does this exactly right (`cmd/aforge/do.go:225`, `:313`,
`:2448`) and the comment above `reportErrand` states the reason: the most common
thing anyone does with a one-shot is pipe it somewhere. That is the standard.
`graph plan` and `graph run` print their preamble to stdout instead
(`cmd/aforge/main.go:424`, `cmd/aforge/run.go:143`) and should not.

`logs` prints the log's path as a first line on stdout and **suppresses it under
`--json`** (`cmd/aforge/logs.go:109`), with a comment saying why. That is the
right instinct; the path is still commentary and belongs on stderr in both modes,
which also makes `--json` stop being a special case.

An interactive question — `cache clean`'s typed confirmation — is written to
stdout today. A prompt is not an answer; it goes to stderr, so that
`aforge cache clean | tee log` still shows you the question.

### Exit codes

**One table, for every headless verb.**

| Code | Meaning |
| --- | --- |
| 0 | it is done, and what is on stdout is the answer |
| 1 | it could not be run at all — no key, bad arguments, the store would not open |
| 2 | it ran and did not finish: part of the work does not stand |
| 3 | a limit you set stopped it — the wall, the token budget, the turn cap |
| 4 | it needs an answer from you and nobody was there |

Today there are three tables. `do` uses 0/1/2 with 2 meaning both "partial" and
"the wall came first". `exec` uses 0/2/3/4/5/6 and never returns 1. `run
subharness` uses 0/1/2 with 1 and 2 meaning the *opposite* of `do`'s 1 and 2 —
`do` 1 is "nothing usable", `run subharness` 1 is "could not be run at all".
A harness that wraps two of these needs two readers, and the person who writes
the second one is going to get it wrong.

`exec`'s existing 2/3/4/5/6 are read by harnesses in the wild, so the migration
carries `AFORGE_EXIT_CODES=legacy` for one release, and **why it stopped is
always in the `stop` field of `--json`** — which is where a script should have
been reading it all along.

### `--json`

Three shapes, and the rule is which verb gives which:

- **A result envelope** — `do`, `exec`, `run`. One JSON object on stdout, always
  parseable, printed even when the run failed.
- **A document** — `graph plan`, `graph revise`. The graph itself, the same bytes
  `--out` would write.
- **A row stream** — `logs`. NDJSON, one object per line, **byte-for-byte the
  bytes on disk**, no header, no trailing sentence. `cmd/aforge/logs.go:105`
  already guarantees this and explains why; do not touch it.

The **result envelope is one shape across all three verbs**:

```json
{
  "ok": true,
  "stop": "done",
  "answer": "…",
  "files": ["…"],
  "error": "",
  "spend_usd": 0.0,
  "tokens": {"in": 0, "out": 0},
  "seconds": 0.0,
  "model": "…",
  "steps": 0
}
```

Today `do` calls the answer `deliverable` and `exec` calls it `text`; `do`
reports `seconds`, `exec` reports `elapsed_ms`; `do` has `settled`, `exec` has
`stop`; neither has the other's spend or usage fields
(`cmd/aforge/do.go:107-138`, `cmd/aforge/exec.go:23-28`). There is no reason for
two shapes and there never was.

The guarantee, written in `--help` and in the manual: **within a release, a field
is never removed and never changes meaning; new fields may appear; `error`
non-empty means the run did not start; `stop` always names why it ended.**
`settled` — a machinery word for "the work is over" — becomes `ok`.

### Interactive or headless

A verb is interactive if it draws a screen or asks a question. Only `chat`,
`resume`, `cache clean` and `rebuild` are, and the last two only to confirm.
Everything else must run identically with no terminal attached. Where a headless
verb needs a decision — raising the spending limit — it reads a preauthorization
(`--yes-spend`, `AFORGE_PREAUTHORIZE_SPEND`) and otherwise exits 4 rather than
blocking on a stdin nobody is holding. `cmd/aforge/run.go:265` already tests for
a terminal before asking; that is right.

### Help

`aforge --help` is **the commands, grouped under the five headings of §2, then
five examples, and nothing else** — sixty lines, one screen and a bit. It is 127
lines today and more than half of it is the environment table
(`docs/design/polish/frames/cmd-help.txt`).

The environment table moves to **`aforge help env`**. It is a reference; it is
consulted, not read; and putting it under `--help` means the last thing on a
person's screen after they ask what the commands are is `AFORGE_CALL_LOG_BODIES`.

Examples belong in `--help` and there are none today. Five, chosen so that each
one teaches a different thing:

```
Examples:
  aforge                                  open the conversation you were having
  aforge do "add a health endpoint and a test for it"
  aforge do "summarise CHANGELOG.md" --json | jq -r .answer
  aforge logs --tail 20 --model anthropic/claude-opus-4
  aforge chat --host devbox:~/src/api      the chat here, the work over there
```

`aforge <verb> --help` prints that verb's line lifted out of the same table, then
its flags, then where the rest is. That machinery already exists and is right
(`cmd/aforge/usage.go:83`) — one source of truth, so a synopsis cannot go stale.

**Asking for help is never a failure**, on any verb, including the ones with no
flags of their own. `exitHelped` and `askedForHelp` are the right answer and are
already written (`cmd/aforge/usage.go:39`, `:230`). `aforge devices --help`
still misses them (`cmd/aforge/chatv3_at.go:312`), and `aforge manual --help`
prints the page list with no usage line above it.

### Emptiness

The emptiness law applies to a command's whole answer, not only to figures. A
listing with nothing in it has **three different answers today**:

- `aforge cache` — "the cache is empty · <path>" (a sentence)
- `aforge services` — absolutely nothing, exit 0 (silence)
- `aforge spend` — `TRIED  COST  LEARNED` and no rows (a header over nothing)

**One rule: a listing with nothing in it prints one short sentence saying so, and
a column header is never printed without a row under it.** Silence and a bare
header both read as a command that broke, which is exactly the failure the law
exists to prevent. `cache` has it right.

## 6. The map between the terminal and the chat

A developer who learned one surface should be able to guess the other. Where a
name differs, the difference must be a real one.

| In the terminal | In the chat | Same thing? |
| --- | --- | --- |
| `aforge` / `aforge chat` | — | the chat *is* the chat |
| `aforge resume` | `/resume` | yes |
| `aforge manual [x]` | `/manual [x]` | yes — same corpus, same arguments |
| `aforge cache` | `/cache` | yes |
| `aforge cache clean --yes` | `/cache clean now` | **the confirming word differs.** One word: `now` in both, because the chat cannot pass a flag. `--yes` stays as the script's spelling. |
| `aforge spend` | `/spend` | yes, once `why self` is renamed |
| `aforge tasks` | `/history` | yes, once `why <node-id>` is renamed. `/history` keeps `/tasks` as an alias so the two surfaces share a word. |
| `aforge logs` | — | terminal only. A model-call log is a developer's tool; it does not need a page in the chat. |
| `aforge models` | `/model` | **different things.** `/model` picks the model you talk to; `aforge models` reports what every model has been measured at. Rename the terminal one `aforge models --measured`… no: keep `aforge models`, and make its *first* line the model this machine will use, with the measurements under it. Then both surfaces answer "which model?" first. |
| `aforge doctor` | `/status` | related, not the same: `/status` is this conversation, `doctor` is this install. Both should say so in their first line. |
| `aforge run <program>` | `/subharness <name>` | yes |
| `aforge devices` | — | terminal only, and deliberately: revoking is this machine's decision (`cmd/aforge/chatv3_at.go:299`). |
| `aforge do "<task>"` | `/task <brief>` | yes — the same engine, one watched and one not. Both help texts should say the other exists. |
| `--yolo`, `--max-hours`, `--max-cost` | `/permissions`, `/budget` | the flags are the launch-time form of the panels |

Verbs with **no chat counterpart and no manual page at all** — `notebook`,
`competence`, `services`, `wake`, `rebuild` — are the resident's, a different
product in the same binary. They stay, they go last in `--help` under
*Housekeeping*, and they get named in the manual (§8).

## 7. The renames, ranked

Each keeps its old spelling as a hidden alias for one release, with a one-line
notice on first use, and changes `internal/manual/chat/` in the same change.

1. **`aforge run subharness <name>` → `aforge run <name>`; the graph pipeline
   moves under `aforge graph …`.** This is the large one and it is the right
   one. `run` names two unrelated commands today, and the proof is in the code:
   `cmd/aforge/usage.go:166` carries a `longerCommands` table whose entire job is
   to stop `aforge run --help` printing the subharness runner's line. When help
   needs a special case to disambiguate a verb, the verb is overloaded.
   `aforge run <name>` then matches `/subharness <name>` in the chat, and
   `aforge graph plan|show|revise|run` groups the four pipeline verbs under the
   noun they all act on.
2. **`aforge why self` → `aforge spend`, `aforge why <node-id>` → `aforge tasks
   <id>`, with bare `aforge tasks` listing.** Nobody types `why` to find out what
   they spent. The chat already calls these `/spend` and `/history`.
3. **`--budget` → `--token-budget`, `--turns` → `--max-turns`, `--max-seconds` →
   `--timeout`, `--ensemble N` → `--passes`, `--contracts` → `--no-method`.**
4. **`-w` → `--dir`, `-o` → `--out`, `-j` → `--parallel`**, single letters kept
   as hidden aliases.
5. **One result envelope and one exit-code table** across `do`, `exec`, `run`.

Nothing else in the surface needs a new name. In particular `chat`, `resume`,
`do`, `exec`, `logs`, `cache`, `doctor`, `manual`, `version`, `serve`,
`devices`, `--model`, `--plan-model`, `--json`, `--yes`, `--host`, `--at`,
`--session`, `--once`, `--yolo` and the `logs` filter family are well named,
consistent, and should be left alone.

## 8. What the manual owes the terminal

`internal/manual/chat/` documents the chat surface exhaustively — a hundred-plus
probes reach it — and documents the terminal barely. Six verbs appear **nowhere
in the corpus**: `why`, `notebook`, `competence`, `services`, `wake`, `rebuild`.
The manual is the only authoritative source about aforge for the model, so a
person who asks the chat "how do I see what that task actually did?" gets an
improvisation or a denial.

Two things follow:

1. A page — `running-from-the-terminal.md` — with a `## ` heading per verb family
   from §2, written in a person's words: *what have I spent*, *what did that task
   do*, *run this without watching it*, *free up disk*, *stop a device*.
2. **The build gate extends to the terminal.** `internal/tui3/manual_test.go`
   checks that every slash command and alias appears in the corpus. The same
   check should run over `knownCommands` in `cmd/aforge/usage.go:285`, so a verb
   added to the dispatch without a page fails the build the way a slash command
   already does. Every rename in §7 must then land its manual edit in the same
   commit — which is the point.

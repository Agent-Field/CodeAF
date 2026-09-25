# Commands you type in a terminal

## Is there a newer version — update a dev build — keep a dev build up to date — latest dev — /update — why does it say this every time I start

At launch, a stable, dev or staging build behind the newest release of its own
channel gets one dim line: `codeaf <newest> is out · you have <running> · /update
installs it and restarts · or: <curl line>`. A release candidate gets that line
when its stable line is published. An equal or ahead build gets no line. Source
and unstamped builds make no launch request, and their `/update` answers `this
codeaf was built from source · rebuild with make build, or install a release:
curl -fsSL https://agentfield.ai/get/codeaf | bash`.

`/update` downloads the release, checks its sha256, replaces this executable and
restarts the same conversation; `/upgrade` is its alias. With no channel word,
a dev or staging build selects its own channel; stable and rc select stable. An
explicit channel or tag wins. Finish a running turn or task first. An ahead dev
build refuses with `this codeaf is <running>, ahead of the newest dev <newest> —
/update <newest> installs it anyway`; naming the tag is the deliberate downgrade.

Set `CODEAF_NO_UPDATE_CHECK=1` to skip only the launch check. Dev and staging
answers are cached beside `config.json` for one hour in `update-check.dev.json`
and `update-check.staging.json`; stable and rc use `update-check.json` for 24
hours. A cached newer release is still shown. The curl line reinstalls this file
from the channel the build follows — dev and staging follow themselves, and a
stable, rc or source build follows stable. So `devaf` gets `curl -fsSL
https://agentfield.ai/get/devaf | bash`, `stageaf` gets `curl -fsSL
https://agentfield.ai/get/stageaf | bash`, a dev build named `codeaf` gets
`/get/codeaf/dev`, a release candidate gets the plain `/get/codeaf`, the same
stable release its launch line just named, and a file under any other name gets
`| CODEAF_INSTALL_NAME=<name> bash` on the end of its channel's line.

## codeaf update from a shell — --check — default channel — ahead of newest

`codeaf update` and `codeaf update --check` default to dev on a `dev-*` build,
staging on a `staging-*` build, and stable on stable or rc. `--stable`, `--rc`,
`--dev`, `--staging`, or `--version <tag>` overrides that choice.

`--check` exits 3 when the selected release is newer, and also whenever a dev or
staging tag is selected from a build of another channel, because a channel tag
cannot be ordered against a build from another channel; that answer reads
`codeaf <tag> is available · you have <running>`. It exits 0 when the
selection is the tag already running, when this build is ahead of its own
channel, and when the two cannot be ordered at all — which is what a dev build
asking `--stable` gets. It exits 1 when it could not check. `--version <tag>`
exits 0 only on that tag and 3 otherwise. Every answer names the selected tag.
A build ahead of its own channel says `the newest dev codeaf is <newest> · this
codeaf is <running>`, with `staging` in place of `dev` on that channel.
Installing without `--check` refuses an implicit downgrade the same way:
`this codeaf is <running>, ahead of the newest dev <newest> — pass --version
<newest> to install it anyway`; naming that tag installs it.

A source build refuses and names its path: rebuild with `make build`, or install
a release with `curl -fsSL https://agentfield.ai/get/codeaf | bash`. An unwritable
target, failed download or bad checksum leaves the original in place and offers
the curl line for this executable and channel; codeaf never tries sudo.

## How do I install codeaf — the curl line, agentfield.ai/get/codeaf, dev, staging, rc and stable channels

The install line is one curl, the same one the README prints:

```sh
curl -fsSL https://agentfield.ai/get/codeaf | bash
```

`https://agentfield.ai/get/codeaf` serves `scripts/install.sh` from the `main` branch of
the public repository, byte for byte. The script under it can be run directly:

```sh
curl -fsSL https://raw.githubusercontent.com/Agent-Field/codeaf/main/scripts/install.sh | bash
```

Pipe it to `bash`, not `sh`: the script uses `set -o pipefail` and `[[ ]]`, and `sh` is
dash on Debian and Ubuntu, which rejects both.

A push to `dev` publishes a `dev-*` build, a push to `staging` publishes a
`staging-*` build, and a push to `main` publishes an rc. Each is marked as a
prerelease. Stable is published only when a person dispatches `Release` on `main`.
`--stable` is the default and reads GitHub's `releases/latest`, which excludes
prereleases. The `/get/devaf` and `/get/stageaf` lines install the dev and staging
channels beside codeaf under those file names. To take another channel, put it on the path —
`https://agentfield.ai/get/codeaf/dev`, `/staging` or `/rc` — or pass `--dev`,
`--staging` or `--rc` after `bash -s --`. A channel with nothing published stops with
`no <channel> build has been published yet`. To pin one complete tag, replace the
final pipe with `| VERSION=<tag> bash`; for example:

```sh
curl -fsSL https://agentfield.ai/get/codeaf | VERSION=v0.2.0 bash
```

For the bare address the proxy hands the script out unchanged; for a channel path
it rewrites the one line that sets the default channel. If it cannot find that line
exactly once, or what it fetched is not a shell script, it answers 502.

Building from source needs nothing published: clone the repository, run `make build`,
then run `bin/codeaf` from the checkout.

The installer writes `~/.codeaf/bin/codeaf` and prints two things: one line
naming the installed file's `version` (`installed codeaf <tag> built …`), and last,
when the folder is not yet on `PATH`, the bare `export PATH=…` line to paste. It
prints nothing about telemetry; codeaf itself shows that notice before any count
is sent.
`/update` in the chat or `codeaf update` in a terminal replaces it in place;
running the install line again works too.

## What is devaf — dev build beside codeaf — side by side — two versions — install under a different file name — --name

`devaf` is the file name for a codeaf dev-channel build, not another product.
Install the newest dev build beside codeaf with:

```sh
curl -fsSL https://agentfield.ai/get/devaf | bash
```

That proxy serves the installer from the `dev` branch and rewrites exactly two
default lines: the channel becomes dev and the installed name becomes devaf. It
writes `~/.codeaf/bin/devaf` and leaves `~/.codeaf/bin/codeaf` untouched. On
Windows the file is `devaf.exe`. `devaf version` still starts with `codeaf`.
The installer's receipt names the command to type first:
`installed devaf · codeaf dev-<date>-<commit> built …`. Any `--name` install
reads the same way, with its own word first.

The general spelling is `--name WORD` or `CODEAF_INSTALL_NAME=WORD`; the name
may contain ASCII letters, digits, `.`, `_`, and `-`, and must begin with a
letter or digit. codeaf and devaf share `~/.codeaf`, including keys and
conversations, and one engine per workspace; opening a workspace with the other
build retires an idle host or joins a busy compatible one. A devaf launch checks
the dev channel hourly, and bare `/update` keeps following dev.

## What is stageaf — staging build beside codeaf — try the next release early — install staging — how often does staging update

`stageaf` is the file name for a codeaf staging-channel build, not another product.
Install it beside codeaf with:

```sh
curl -fsSL https://agentfield.ai/get/stageaf | bash
```

That proxy serves the installer from the `staging` branch and rewrites only the
channel and name lines. It installs `~/.codeaf/bin/stageaf` (`stageaf.exe` on
Windows) beside an untouched codeaf. `stageaf version` still starts with `codeaf`.
It shares `~/.codeaf` with codeaf and devaf, including keys and conversations.
A stageaf launch checks the staging channel hourly; bare `/update` follows staging.

The promotion runs once a week and moves staging to the newest dev commit at
the Friday 17:00 Toronto-time cutoff when the full check passes. When the check
fails or there is nothing new, staging stays where it was. A person still
decides when main moves.

## Why codeaf do may download rtk — compressed shell output and how to turn it off

When `codeaf do` starts local workers, it starts one background attempt to find or
fetch rtk v0.45.0. rtk is a third party's program from `github.com/rtk-ai/rtk`; it
compresses the output of its named read and check filters before the work model pays
to read it. Anything that writes runs plain, and a wrapped result codeaf cannot trust
is run again without rtk.

If no rtk is already resolvable and `CODEAF_RTK` is unset, codeaf downloads
`rtk-<target>.tar.gz` and `checksums.txt` from that repository's release. The whole
fetch has two minutes and every downloaded response is capped at 64 MB. Its sha256
must match the published checksums; a mismatch or missing entry is refused. The
managed fetch supports only macOS arm64, macOS amd64, Linux amd64, and Linux arm64;
on any other platform it fetches nothing. A copy you name or put on `PATH` can still
be used there.

The archive contributes only its file named `rtk`. It is installed atomically with
mode `0700` at `$CODEAF_HOME/bin/rtk`, or `~/.codeaf/bin/rtk` when `CODEAF_HOME` is
unset or empty. Nothing waits for it: commands run plain until it lands. Success logs
once per process as `note: installed rtk v0.45.0 for compressed shell output`; failure
logs once as `note: shell output will not be compressed — <err>`.

`CODEAF_RTK=off` disables rtk everywhere. Set `CODEAF_RTK` to an executable path to
use your own; naming a path that is not executable also prevents fallback and fetch.
Without that variable, codeaf looks for `rtk` on `PATH`, then its managed copy. Calls
to rtk carry `RTK_TELEMETRY_DISABLED=1` and `RTK_NO_TOML=1`: codeaf does not opt you
into a third party's collection.

## Running codeaf from the terminal — can I run this without the chat

Typing `codeaf` with no arguments opens the conversation. Everything else is a verb after
it, and there are five kinds:

```
talk to it              chat · resume
hand it work            do "<task>" · exec "<prompt>" · run <program>
look at what happened   why self · why <task-id> · notebook · competence · services ·
                        logs · models · doctor · manual · version
housekeeping            connect · disconnect · cache · cache clean · rebuild · wake ·
                        serve · devices · help env
plan work by hand       plan new "<goal>" · plan show <plan.json> ·
                        plan revise <plan.json> "…" · plan run <plan.json>
```

One more verb is the plan store the bash-belt task worker coordinates through, exposed
to a person: **`codeaf plandb`** answers the same commands the worker runs in bash —
`codeaf plandb list --status ready`, `codeaf plandb task overview`, `codeaf plandb
critical-path` — against the run's own `plandb.db`, so a plan a worker is driving can be
read the way the worker reads it.

Two more exist and are deliberately kept out of the help text, because nothing types them
by hand: **`codeaf engine`** is the far half of `chat --host`, started by ssh, and
**`codeaf tick`** is the one bounded pass the background timer runs every five minutes.
Neither draws anything or reads a key.

## Connect from the terminal without opening the chat — codeaf connect and codeaf disconnect

`codeaf connect` lists every model service this profile knows, whether it is connected,
and whether its door is a browser or a key. `codeaf connect codex` signs a ChatGPT plan
in through the browser; `codeaf connect openrouter` uses OpenRouter's existing browser
road. Add `--no-browser` to print the address without opening it. For Codex on another
machine, the next line gives the tunnel to run before opening that address here:

```
ssh -L 1455:localhost:1455 <that machine>
```

If that sign-in chose port 1457 instead, the printed command uses 1457. DeepSeek and
MiniMax take a key through the same checked connection as `/connect`; Ollama takes none.
Z.ai, Moonshot and Qwen take a key and also need `--region intl` or `--region cn`. A key
is read from stdin when it is piped, or asked for without echo on a terminal. A new custom
service is created only in the chat: an unknown custom name says it is not a service this
profile knows. Once the chat has created one, `codeaf connect <its name>` can reconnect
that instance with a key.

`codeaf disconnect <service>` forgets the connection and its key or sign-in. Neither
command sends a prompt, calls a model or adds model spend. They do not print keys or
tokens.

## The belt's hands from a shell — codeaf patch, codeaf doc, codeaf web fetch, codeaf web search, codeaf image

Four verbs reach, from a terminal, the same hands the conversation's model uses — each
through the same code path the tool on the belt runs, so the two cannot drift:

- **`codeaf patch FILE --old TEXT --new TEXT`** is the edit hand's exact-match
  replacement: the old text must match exactly one region of the file, the file is
  rewritten with that one region replaced, and a refusal names how many regions matched
  and leaves with 1 without writing anything. `--old-file` and `--new-file` read the two
  texts from files when they are awkward to quote.
- **`codeaf doc PATH [--pages A-B]`** reads a document the way the conversation's
  `read_document` does: locally and free when the file has a text layer, otherwise
  through the billed parser rungs on your profile's key. A plain text file is printed as
  it is, with no call at all. `--pages` names pages of a PDF the local rung reads; billed
  text arrives with no page boundaries, so a range on a scan is refused.
- **`codeaf web fetch URL`** and **`codeaf web search QUERY`** are the belt's web verbs:
  one page fetched with the markup stripped and bounded the way `web_fetch` bounds it,
  or one search rendered as the numbered list `web_search` renders, on whatever provider
  the settings name.
- **`codeaf image PROMPT --out PATH [--model M]`** generates one picture where the
  `generate_image` tool does and writes it at PATH, printing the path. The spend is
  recorded the way the tool records it — one row in the usage ledger — so a run's books
  see it.

`patch` spends nothing, and so does `doc` on a plain file; `doc` on a scan, a search or a
fetch on a keyed provider, and every `image` call the model and are billed like any
other call.

## What codeaf --help prints — the five groups, and where the environment table went

`codeaf help`, `--help` and `-h` all print the same thing: every command under those five
headings, in that order, then five worked examples.

**The environment table is not on that page**: it is `codeaf help env`, because it is a
reference somebody consults and it used to be more than half of what `--help` printed.

Every verb also answers `<verb> --help` with its own line and its flags, and `codeaf plan
--help` answers with all four of its subcommands.

## What $? means after a headless one-shot — the codes it leaves with

**One table, and `codeaf do`, `codeaf exec` and `codeaf run` all leave on it.**
This is what `$?` holds after a one-shot, and it is the thing a script should branch on:

| `$?` | what it means |
| --- | --- |
| 0 | it is done, and what is on stdout is the answer |
| 1 | it could not be run at all — no key, bad arguments, the store would not open, the name is not a program |
| 2 | it ran and did not finish: part of the work does not stand |
| 3 | a limit you set stopped it — the wall, the token budget, the turn cap, the price |
| 4 | it needs an answer from you and nobody was there |

The three commands used to have three tables, and two of them meant opposite things by the
same number: `do` exit 1 was "nothing usable came back" and `codeaf run` exit 1 was "it
could not be run at all", while `exec` returned 2, 3, 4, 5 and 6 and never returned 1. If
you have a script written against the old numbers, `CODEAF_EXIT_CODES=legacy` puts `exec`'s
back for one release — see below — and the rest is in
`docs/design/polish/envelope-and-exits.md`, which sets old and new side by side.

**A wall on the default run road leaves with 124.** When `--timeout` ends a `codeaf do`
on the worker harness, `$?` is 124 — the number `timeout(1)` uses — rather than 3, and
`stop` still says `deadline`. The run engine reports a wall and a failed part with the
same word, so the number is where the two are told apart.

**Exit 3 and exit 2 are different questions.** 3 says the work was going when something you
set cut it off, so raising `--timeout`, `--token-budget` or `--max-turns` and running it
again is the remedy. 2 says it got to the end and part of it does not stand, so what came
back is worth reading before anything is re-run.

**A delivery nothing checked is exit 2 too, and it says so.** `codeaf do` puts what a run
delivered to a final check; when that check cannot be reached — a dead route, a refused
account, a service that is down — the work still ships (holding a finished deliverable
hostage to the weather helps nobody), and the run ends `unchecked` rather than `ok`. The
gate is asked once more first, inside the wall, unless there is no time left for a call or
the refusal was about the request itself and every endpoint would say the same. The last
line on stderr is

```
delivered without a check: the gate could not be reached
```

with how it was asked and the provider's own sentence after a `·`. `$?` is 2, `stop` is
`unchecked`, `ok` is false, and `--json` carries the whole reason in `unjudged`. **Read the
answer — it may be perfectly good — but nothing has vouched for it.** Two graded runs
ended `ok` at exit 0 over exactly this before the ending had a name.

**Exit 1 means nothing ran, and only that.** It is the rung for a refusal *before* the work
starts — no key, a flag it could not parse, a store that would not open, a saved program by
that name that does not exist. Nothing was attempted, so nothing was spent and there is
nothing on stdout worth keeping. **A run that started and then failed leaves with 2, not
1**, however early it broke: a provider that gave up at turn nine has already cost you
money and is usually holding part of an answer, and the reason it stopped is in
`incomplete` rather than in `error`. So a script may retry exit 1 blind, and must not do
that with exit 2 — read what came back first. `codeaf exec` published mid-run provider
failures as exit 1 for a while; if a wrapper of yours treats 1 as "provider flaked, try
again", that is the line to revisit.

**Exit 4 is the one nobody can fix by retrying.** A headless run has no keyboard, so a
question ends it. The question is printed on stderr verbatim, under `it stopped to ask:`,
and it is in the `blocked_on` field of `--json`. Answer it inside the ask itself and run it
again, or bring the work to `codeaf` where it can be answered.

Asking for help is never a failure: `--help` on any verb exits 0.

## The --json result object — one shape, three commands

`--json` on `codeaf do`, `codeaf exec` and `codeaf run` prints **one object on
stdout, always parseable, printed even when the run failed**:

```json
{
  "ok": true,
  "stop": "done",
  "answer": "…",
  "files": ["notes.md"],
  "error": "",
  "spend_usd": 0.0213,
  "tokens": {"in": 18422, "out": 1130},
  "seconds": 91.4,
  "model": "anthropic/claude-opus-4",
  "steps": 3,
  "run": "0123456789abcdef",
  "calls": 47,
  "rounds": 2,
  "redispatches": 1
}
```

| field | what it holds |
| --- | --- |
| `ok` | the work stands. True on exactly the runs that exit 0 |
| `stop` | why it ended: `done`, `error`, `incomplete`, `unchecked`, `budget`, `turn-cap`, `deadline`, `price`, `question` |
| `answer` | what was produced, in prose. Empty when nothing was |
| `files` | the paths it wrote. Never null — a run that wrote nothing carries `[]` |
| `error` | why it could not be run at all, in the same words stderr carried. Empty on every run that started, however it ended — a limit that cut a run short and a provider that failed mid-run both say why under `incomplete` |
| `spend_usd` | what it cost, whole, in dollars |
| `tokens` | `{"in": …, "out": …}` |
| `seconds` | wall clock |
| `core_done_seconds` | when the requested work was first found done, in seconds from the start; absent when the gate never said so |
| `model` | the model the work ran on |
| `steps` | how many pieces of work ran — `do`'s nodes, `exec`'s turns. A saved program does not measure it: the key is still there, holding `0`, and that `0` is a measurement nobody took rather than a count of none |
| `run` | this invocation's id. It names the folder `--debug` writes into, and every row this run wrote into `~/.codeaf/logs/calls.jsonl` carries it too — so `codeaf logs --run <that id>` is how you get from this object to the calls behind it. Empty on a verb that opened no run of its own |
| `calls` | how many model calls the run made, counted whether or not the call log is switched on. It is the figure you would otherwise count by hand in `calls.jsonl` |
| `rounds` | how many times the run went back for **more work** after looking at what it had. One is the ordinary shape; eight is a run that kept finding more to do, and it is the number that explains a bill nothing else here accounts for. `exec` does not plan and a saved program does not grow, so both hold `0` — a measurement nobody took, the way `steps` does |
| `redispatches` | how many times one of the run's nodes was sent round again **in place** after running out of the room it was granted — back on the queue to carry on from what it had already banked, rather than grown around. Present only when it happened: a run that never re-dispatched a node carries no key at all, and neither does a verb with no plan nodes |

**Within a release a field is never removed and never changes meaning; new fields may
appear.** `stop` is the field to read for *why*; the exit code only says how much is wrong.

**None of the three takes `--yolo`.** That is the conversation's flag, and it means "stop
asking me before each tool call" — these three have nobody watching in the first place, so
nothing in them stops to ask. Typing it is refused by name:

```
error: codeaf do has no --yolo flag — nothing here stops to ask, and --yes-spend answers the one question a run can still stop on
```

`--yes-spend` is the nearest thing to an equivalent on `do` and `run`: the one thing they
still refuse is a plan whose price crosses your limit. `exec` has no such flag — what bounds
one pass there is `--token-budget` and `--timeout`.

`--json` on `codeaf plan new` and `codeaf plan revise` is a different thing: it is the plan
itself, the same bytes `--out` would write. `codeaf logs --json` is a third: one JSON object per line,
byte-for-byte what is on disk.

## Which models a headless run uses — the crew, --best, --cheap, --pin and the daily cap

A headless run has the same crew a conversation's task has: a **worker**, a **planner** and
a **checker**, and every seat nobody pinned is picked for this task from what kind of work
it is. The run says its crew on stderr before anything is spent — every seat, and which
rung answered it — and under it the class the task was read as and the estimate:

```
models: worker z-ai/glm-5.3-flash (routed) · planner z-ai/glm-5.3-flash (routed) · checker moonshotai/kimi-k3 (pinned)
crew: bugfix · worker glm-5.3-flash (openrouter) · checker kimi-k3 (pinned) · est $0.023
```

A seat you pinned reads `checker kimi-k3 (pinned)` on the headless crew line.
When the run ends, the `crew:` line is said again with what it actually cost beside the
estimate: `crew: bugfix · worker glm-5.3-flash (openrouter) · checker kimi-k3 (pinned) · $0.021 (est $0.023)`.

**Every model flag is a one-task pin.** `--model`, `--plan-model` and `--check-model` pin the
worker, planner and checker for this run and no other, and `CODEAF_MODEL`,
`CODEAF_PLAN_MODEL` and `CODEAF_CHECK_MODEL` do the same from the environment. Each seat
climbs its own ladder — the flag, then its variable, then a `/crew pin` in the profile,
then the crew picked for this task — and the rung that answered is named beside the seat
(`--model`, `CODEAF_MODEL`, `pinned`, `routed`). **The checker never inherits the planner:**
a `--plan-model` says who plans and nothing about who checks.

`codeaf do` takes three more, all for this one task:

- **`--best`** — the strongest crew your allowed models make.
- **`--cheap`** — the cheapest crew your allowed models make.
- **`--pin seat=model[@provider]`** — pin one seat, `worker=`, `planner=` or `checker=`; say
  it once per seat. `--pin checker=moonshotai/kimi-k3@openrouter` sends the checker through
  that connection. A seat that is not one of the three, or a pin with no model, is refused.

Nothing a headless run is told is written to the profile: the pins and the allowed models
stay what `/crew` last set.

**At the daily cap, `codeaf do` refuses.** When today's crew spend has reached the cap
`/crew cap` set, it starts nothing and says so:

```
today's crew spend has reached the daily cap of $5.00 · raise it or turn it off with `/crew cap`, pass -yes-spend, or wait until midnight
```

`-yes-spend` is the one way past it for this run — past the daily cap, never past the
per-task limit. `--cheap` is refused at the cap like any other run. The day turns over at
midnight on this machine's clock.
**Every run is held to the per-task limit**, $5 unless `/crew cap task` set another: a call
that would take the run past it is not made, and the run stops on
`this task reached its $5 limit · raise it in /crew`. The other headless doors — `exec`, `run`, `plan run` —
are a person at a terminal running one thing, so they **warn and go on**:
`note: today's crew spend has reached the daily cap · this run goes ahead; `codeaf do` would have stopped`.

With `--json`, `codeaf do` carries the crew too: `class` (the kind of work the task was read
as), `crew` (each seat's `model`, `provider`, `kind`, `pinned` and `est_usd`; `crew.<seat>.pinned` is true for a pin), `est_usd` for
the whole crew beside `spend_usd`, `effort` when `--best` or `--cheap` was given, and
`check_model` with `check_model_source` beside the worker's `model_source` and the planner's
`plan_model_source`.

## What checked my unattended or headless run — what judged the delivery, and why task.audit is not the answer

A `codeaf do` errand's delivery is read at the end by the **delivery gate**. It takes a
reading of the project's own checks before the work and another at the end, maps what you
asked for onto the checks that exercise it, and answers whether the delivery holds.

With `--json`, `judged_by` names that reader when the settled root has a gate row that
is not marked unreachable. An unreadable response still names the reader; read `ok` and
`stop` to learn the outcome. `unjudged` instead names an unreachable gate's reason. A run
with no gate row can omit both keys, so absence alone does not prove a check happened.

`task.audit` is a different road's row and does not reach `codeaf do`. It governs work the
conversation hands out with `/task`: a separate, fresh, read-only checker is put in a clean
restore of what the task wrote. Turning that row off produces the report line `nothing
checked this work: the task.audit setting is off`. A headless errand never prints that line,
because the session task engine is not the engine running it; its delivery gate is the
check.

One limitation remains: when a job is broken into several pieces, its gate is journaled
against the piece that delivered rather than the whole that settles them, so `judged_by` is
absent there.

## The old --json field names — deliverable, text, elapsed_ms, settled

**The old names still work, for one release, and then go away.** They are printed beside
the new ones, so nothing that reads them breaks today and nothing has to be rewritten in a
hurry:

| old name | read this instead |
| --- | --- |
| `deliverable` (`do`), `text` (`exec`) | `answer` |
| `artifacts` | `files` |
| `spend` (`do`) | `spend_usd` |
| `nodes` (`do`), `turns` (`exec`) | `steps` |
| `elapsed_ms` (`exec`) | `seconds` |
| `usage` (`exec`) | `tokens` and `spend_usd` |

**`settled` is not the old name of `ok`, and it is not going away.** It means "nothing this
run is waiting for can still move", which is true of a run that asked a question and did
nothing: `settled: true` with `ok: false` and exit 4. Reading the one as the other would
record every refusal as a success. A broken finished tree is the one ending that answers
that sentence false; *Why settled can be false* below gives its exact shape.

## Why settled can be false — the tree does not build or the run left code broken

A run that hands back a tree its own check could not collect is not settled: the tree does
not build, it left the code broken, and repairing it is still work waiting to move. That
run says `settled: false`, `ok: false`, `stop: "incomplete"`, and leaves with exit 2. Its
answer includes the check's own sentence about what could not be read.

## Fields that belong only to one command

Some fields belong to one command and stay. `codeaf do` carries `spend_work` and
`spend_overhead` — what the work cost against what it cost to decide what the work should
be — and `blocked_on`, `learned`, `plan_model`, `model_source`, `plan_model_source`,
`check_model`, `check_model_source`, `class`, `crew`, `est_usd`, `effort` and `subharness`. It also carries `judged_by` when the settled root records an answered gate
attempt and `unjudged` when that gate could not be reached. Both keys can be absent when
no root gate row is available; neither key replaces `ok` and `stop`. `codeaf run` carries `output`, which is
the typed answer whole, and `report`.

`incomplete` is on `codeaf run` and `codeaf exec` both, and it is why it did not finish, in
the same words stderr carried — a token budget that ran out with half an answer already
written, a wall that arrived, a provider that gave up at turn nine. It is **not** `error`:
`error` means the run could not be started at all, and every one of those started. A run
that was cut short leaves `error` empty, puts what it managed in `answer`, names the reason
in `stop`, and says the sentence in `incomplete`.

## Keeping exec's old numbers for one release — the legacy switch

`codeaf exec` used to leave with 2 for the token budget, 3 for the turn cap, 4 for the wall,
5 for an error and 6 for a run that finished with nothing to show, and it never returned 1.
Those five numbers all moved when the three commands were put on one table.

Setting `CODEAF_EXIT_CODES=legacy` puts them back:

```
CODEAF_EXIT_CODES=legacy codeaf exec "…"
```

**It changes nothing else.** Not `codeaf do`, not `codeaf run`, not one field of `--json`,
not one word on stderr. It is an escape hatch for scripts already written, it applies to
`codeaf exec` and to nothing else, and it goes away after one release. The thing to change
the script to is `stop` in `--json`, which names why a run ended in a word rather than a
number and is the same word on all three commands.

## What does codeaf wake do — running the background pass by hand, once

`codeaf wake` does one bounded pass of the work that normally happens on the five-minute
timer, prints what it did, and exits. **It starts no permanent process and no worker
runner.** Unlike everything else on this page that only reads, it spends: it makes model
calls.

```
codeaf wake [--db path] [--timeout 2m]
```

`--timeout` defaults to **2m**. It takes a duration — `--timeout 5m`, `--timeout 90s` — and
a bare number is still read as seconds, so `--timeout 120` is the same wall. Zero or less
is refused at the flag: `--timeout: must be positive`. Inside that wall it takes at most
**32** passes, and stops early the moment a pass changes nothing.

The flag used to be `--max-seconds`, which was the same wall spelled a third way and in the
unit rather than in the quantity: `codeaf wake --max-seconds 5m` was a parse error on a
machine where `codeaf do --timeout 5m` works. **`--max-seconds` still works for one
release** and says so on stderr the first time it is used:

```
note: `--max-seconds` is now `--timeout` — the old spelling works for one more release.
```

**It defers to a live one.** If something else already holds the role for that store, it
prints one line and does nothing:

```
resident alive (pid 41207) — skipping wake
```

The exception is a holder that is alive but has stopped completing passes. Waiting on that
forever is how a stalled process quietly stops every check on the machine, so it says so
and goes anyway:

```
resident (pid 41207) has not ticked since 2026-09-02T11:04:18Z — waking anyway
```

The last line counts what the pass did:

```
examined 3, checked 2, fired 1, no 0, errors 0, rail waits 0, practice 0, learning 2
```

## What did that task actually do — see one piece of work's turn-by-turn record, with codeaf why

This prints one piece of work's whole record: every turn, what it said, every tool it
called with its arguments, what came back, and how it ended.

```
codeaf why <node-id> [--db path]
```

Each entry is a headline with its body indented four spaces under it:

```
turn 1 · said
turn 2 · read ←
turn 2 · read → 12ms
turn 3 · bash → 1.4s · error
turn 4 · codeaf
turn 5 · stopped
turn 6 · the record stops here
```

`←` is the call going out; `→` is what came back, with how long it took — milliseconds
under a second, tenths of a second above. `· error` marks a tool that failed. A turn signed with the product's own name is a note
about the run rather than something the model said, and `the record stops here` means the
record was trimmed.

**When there is nothing to print it says which nothing it is**, because those are two very
different situations:

```
build has no transcript: either nothing has run it yet, or the worker that ran it keeps no record.
```

**And it leaves with 1**, not 0. The sentence is for you; the exit code is for the script
that asked, which would otherwise read a success and conclude the id exists and has nothing
in it. `codeaf logs --run <id>` answers a miss the same way.

The id is the one the plan gave that step — the same id `logs --node <id>` filters on, and
the value of the `node` field in `logs --json`.

## The run finished and my directory is empty — where did the work go after codeaf do spent money and wrote no files

`codeaf do` edits the directory you point it at in place; it files nothing anywhere else.
When a run did work but ended without delivering or creating or changing a file, its answer
ends with:

```
No created or changed files were recorded: this run worked in /srv/project, editing it in place.
```

A run that did write instead names every file by its absolute path.

The run's own record is separate. On a run that does not finish cleanly, the
`record kept at <path>` line on stderr names the private store holding its traces and job
logs. The empty file record concerns the project directory. It does not prove the directory is
unchanged: deletions and files outside the bounded workspace scan may be absent. Under
`codeaf do --json`, `workspace` carries the same absolute project directory. It is empty
when an existing resident did the work and this invocation cannot establish its directory.

## Where is the record of my headless run — reading a kept one-shot's store

`why` reads a store, and by default that store is `~/.codeaf/graph.db`. A headless
`codeaf do` run does **not** work there: it uses a separate store of its own, kept only when
the run failed or you asked for it with `--keep`, and the last line on the error stream
says where:

```
record kept at ~/.codeaf/runs/codeaf-do-3f81c2
```

That directory holds a `graph.db`, and that is what to point the reader at:

```
codeaf why <node-id> --db ~/.codeaf/runs/codeaf-do-3f81c2/graph.db
codeaf why self      --db ~/.codeaf/runs/codeaf-do-3f81c2/graph.db
```

`--db` means the same thing on `why`, `notebook`, `competence`, `services`, `doctor`,
`rebuild`, `wake` and `do`. **None of them will create a store**: a `--db` that is not a
regular file is refused, each in its own words — `open receipts: <path> is not a regular
database file` from `why`, `open notebook: …`, `open competence map: …`, `open store: …`
from `rebuild`, `open wake store: …`.

**A conversation's tasks are somewhere else entirely.** A task commissioned in the chat
writes its transcript to a file beside the conversation, not into this store, and its room
in the chat replays it. `why` is the reader for headless work and for the background pass.

## What have I spent today — the receipts, with codeaf why self

`codeaf why self` prints today's receipts: what was attempted, what it cost, and what was
learned from it. The day is local midnight to now.

```
codeaf why self [--db path]

TRIED                          COST     LEARNED
Practice parser recovery       $0.31    facts #14,#15; surprise down 12%
Summarise the changelog        $0.0042  nothing
```

- **TRIED** is the intent the work was started with, folded onto one line.
- **COST** is dollars, to cents, and to four decimals when it is under a cent.
- **LEARNED** lists `facts #…` and `skills #…` by their notebook numbers, then
  `surprise down N%` when the work became more predictable. When nothing came of it the
  column says `nothing`, and work that became *less* predictable adds `surprise up N%`
  after it.

**A day with nothing on it says `nothing was tried on its own account today.`** It used to
print the `TRIED  COST  LEARNED` header with no rows under it, which reads as a table whose
rows failed to arrive rather than as a quiet day.

## What has it learned — reading and retracting beliefs with codeaf notebook

`codeaf notebook` prints every belief in the store, newest last, with the evidence for it:

```
SEQ  SCOPE      KIND        AGE     USES  RIDES  BAD  STATUS       BELIEF
#14  tool:git   lesson      2d ago  6     4      0    active       always verify changes
#15  user       preference  5h ago  1     0      0    quarantined  prefer compact tables
```

`USES` is how often it was retrieved, `RIDES` how often it rode along into a piece of work,
`BAD` how often that work then failed. `STATUS` is one of `candidate`, `active`,
`superseded`, `quarantined`. Under the table is the day's spending line — one of
`daily rail: unlimited`, `daily rail: $500.00`, `today's spend: $1.23 of $500.00 daily rail`
or `today's spend: $1.23; daily rail unlimited`. A day that has spent nothing prints no
figure for it. Scope renamings follow under `aliases`, as `from → to`.

**To take one back:**

```
codeaf notebook retract 15      quarantined #15: prefer compact tables
codeaf notebook restore 15      restored #15: prefer compact tables
```

Retracting hides a belief from everything that would otherwise reach for it; restoring puts
it back. Nothing is deleted and the number never changes. A number that is not there is
refused with `notebook fact #15 not found`, and anything that is not a positive number with
`invalid notebook fact sequence "x"`. The `#` is optional. Any other word gets
`unknown notebook command "forget"`.

## What is it actually good at — the measured evidence, with codeaf competence

`codeaf competence` sorts what this machine has evidence about into four groups, printed in
this order: `strong`, `frontier`, `weak`, `stale`.

```
strong
  repo:/work/parser — 24 runs · 4% failed · surprise flat · 2 installed skills
frontier
  linear work — 6 runs · 33% failed
stale
  repo:/work/old-api — last touched 3w ago
```

Each row names what the evidence is about, then the evidence: how many runs, what share of
them failed, which way surprise is trending, and how many skills are installed there. A
scope with skills that has never been exercised says `installed, not yet exercised` instead
of a failure rate. A `stale` row says only when it was last touched, because an old
measurement is not a measurement.

`--model <slug>` picks whose measured behaviour to fold in; left alone it is the model this
machine would run work on. **On a machine with no evidence the whole answer is one line:**

```
No competence evidence yet.
```

## The Model Pool — what this machine reads from it, with codeaf pool

codeaf picks its models against the public Model Pool: a signed index of
measured models that your runs improve. Nothing about your code ever leaves
the machine — what is shared is a measurement of the run, not the work. One
setting answers for all of it, `model_pool` in `/settings`, with three
values: `on` reads and sends, `read` uses the pool and sends nothing, `off`
does neither. It defaults to `on`. `CODEAF_MODEL_POOL` pins the same word
from the shell, and on a CI machine with neither set codeaf reads but does
not send. **The telemetry off switch stops the pool sending too**:
`CODEAF_TELEMETRY=off`, `DO_NOT_TRACK=1` or `codeaf telemetry off` caps the
pool at `read` — it wins over an explicit `on` — and `codeaf pool status`
then says `mode read · telemetry`.

```
codeaf pool [show|status|verify] [--json] [--key key]
```

`show` — also what bare `codeaf pool` prints — is the reading form: the mode
and the addresses in force with the word saying where each came from
(`default`, `setting`, `env`, `ci`, or `telemetry` when the telemetry off switch capped
sending), then what index is cached, how old
it is and how many cells it holds, or `no index cached yet · built-in
seed of <date>`. The binary carries a seed index of our own scored runs,
read until a fresher signed one is cached. `--cells` lists the held
index's cells, one per line — the role, the model, the dims the cell
spells, the measurement and the installs behind it — and `--json
--cells` carries them as an array. Your install also keeps
the scores its judge gave in `own.json`
under the pool directory — `show` and `status` say what that sheet holds — and
the crew reads them beside the index. `status` adds how many rows are waiting to be sent
and whether the mode allows sending and reading; `codeaf telemetry show` prints the
rows themselves, as JSON. `codeaf pool status` also
says whether the relay answered, and whether the mirror did, and what the
last judge did — which model, which seats it scored, or why it failed. `--json` prints
the same answer as one object; `show` reads nothing off the network.

**The scores start here.** In a conversation, after a task lands, a model
outside the crew is asked to score each seat the work ran on — the worker that
carried it, and the seat that checked it when there was one. The scores stay
in your install's own sheet (`own.json`) and are read when the next crew is chosen;
nothing else reads them. With `model_pool` set to `on` the same scores also
wait in `outbox.jsonl` beside the sheet, to leave with the pool's other
measurements; `read` keeps them local, and `off` asks no judge at all and
writes nothing. The call itself is billed to the `judge` seat, so it shows up
in the spend pages beside the crew seats rather than inside a task's own
cost. The judge asks with the key the install holds at that moment, so a task
that lands just after you paste a key into first-run setup is scored with that key.
A task that lands while there is no key at all is not judged. It is not marked
judged either, so the next start scores it once a key exists.

With `model_pool` on, the rows leave for the relay after each judged run and
once more at start-up, under this install's own nonce and nothing else. The
index is fetched once a day, checked against the key built into the binary —
or the key in `models.pool.public_key` when one is set — and a changed
document is read at the next start.

`verify` fetches a fresh index and checks its detached ed25519 signature,
then prints the version whose signature checked out:

```
signature good: version 7, generated 2026-09-10, 3 metrics
```

It wants a public key: `--key <base64 ed25519 public key>`, repeatable, or
one built into the build. The build carries the index signer's key, so
`verify` works as it stands; `--key` checks under that key alone — the
build's key is not consulted beside it, so a document signed under any
other key does not verify. A fetch or a signature that fails is exit 1; a
`verify` on a machine whose setting is `off`, or whose
`models.pool.public_key` does not decode, is refused with exit 2 and
fetches nothing.

A private relay is a copy of `relay/` deployed to your own account with your
own keypair, and `CODEAF_MODEL_POOL_RELAY_URL` with `models.pool.public_key`
(or `CODEAF_MODEL_POOL_PUBLIC_KEY`) point an install at it.

## What is still running in the background — codeaf services, and stopping one

`codeaf services` lists the long-running processes started here and not yet stopped — a dev
server, a watcher — one per line, tab separated: the name, the status, how long it has been
up, what is watched for health, and where its log is.

```
dev-server	running	2h	port:5173	/tmp/dev.log
```

**With nothing running it says `nothing is being kept running.`** It used to print
absolutely nothing and exit 0, which is indistinguishable from a command that broke — so it
answers in a sentence now, the way `codeaf cache` always has.

To stop one, name it:

```
codeaf services stop dev-server

dev-server	stopped
```

A name nothing answers to is refused — `service "dev-server" is not running` — and anything
other than `stop <name>` gets the usage line
`usage: codeaf services [--db path] | codeaf services stop <name> [--db path]`.

## codeaf rebuild — throwing away everything worked out from the journal and replaying it

Everything in the store except the journal was worked out from the journal, and can be
thrown away and worked out again. `codeaf rebuild` is that, and it is the recovery path
when something in the store looks wrong.

```
codeaf rebuild [--db path] [--yes]
```

**It asks first**, on two lines, and only `y` or `yes` proceeds — anything else, an empty
line included, says `cancelled` and changes nothing:

```
Rebuild everything codeaf worked out from the journal in /home/you/.codeaf/graph.db?
The journal itself is untouched; everything worked out from it is discarded and replayed. [y/N]
```

**The question is on the error stream, and so is `cancelled`.** Only the line saying what
was replayed goes to stdout — so `codeaf rebuild | tee log` still shows you the question
and still lets you answer it, and the file gets the result and not the prompt.

`--yes` skips the question for a script. When it is done it says what it replayed:

```
rebuilt 128 steps from 4173 journaled events
```

**It refuses while another process holds that store**, because the rebuild is one
transaction but the process on the other side would be reasoning about a graph that moved
underneath it:

```
a resident is running (pid 41207) — close it before rebuilding
```

Conversations, settings and credentials were not worked out from the journal and are not
touched.

## codeaf do is waiting and nothing happens — machine busy on a headless run

**A `codeaf do` the machine is holding says so on stderr, once.** The same two limits
that hold a task in the chat hold its workers here: `task.max_load` (load per core)
and `task.min_free_mb` (available memory). The chat draws `waiting · machine busy` on
the run's rail; a headless run has no rail, so the first time a worker is held,
stderr gets one line in the same words and the limit that held it:

```
waiting · machine busy · load per core at or above task.max_load 1.5
waiting · machine busy · available memory under task.min_free_mb 1536 MiB
```

Those figures are the defaults; the line prints your own. It is not repeated while
the machine stays busy. When a held worker starts, stderr says `starting · the machine has room again`,
once, and the run goes on as usual.

**If `--timeout` arrives first, nothing started.** The run leaves with 124 and `stop`
says `deadline`, as every wall does; `--json` also carries `blocked_on`, the held line
followed by ` · nothing started before --timeout`, so a script can tell a run the
machine held from one that was slow. Nothing was spent.

To let it start, wait for the machine to clear, raise `--timeout`, or set that row
to `0` in `/settings`, which turns that half of the check off. On a machine with no
`/proc` (macOS, Windows) the limits never hold, and this line never appears.

## What goes to stdout and what goes to stderr — piping a headless command

**stdout is the answer. Everything else is on stderr.**

The answer is the thing you would capture: the deliverable, the `--json` object, the rows of
`codeaf logs`, the plan `codeaf plan show` prints. Everything a person reads *about* the
run is on stderr: the `goal:` and `models:` preamble, the progress lines, warnings, the
path a record was kept at, the receipt saying a file was written, and any question the
command asks you. For `codeaf chat --once`, this includes every handover and ending line:
`finishing here · what was asked is done`, `stopping here · ` and the line that says a
reply's work was moved. Landing-woken replies remain on stdout with the first reply.

So these do what you would expect, and nothing has to be filtered out of them:

```
codeaf do "summarise CHANGELOG.md" --json | jq -r .answer
codeaf plan new "ship the endpoint" --json > plan.json
codeaf plan run plan.json > result.txt          # the preamble stays on your terminal
codeaf logs --tail 20 | wc -l                   # 20, not 21
codeaf cache clean | tee clean.log              # you can still see the question
```

Three of those used to be wrong. `codeaf plan new` and `codeaf plan run` printed their
`goal:`/`workspace:`/`models:` preamble into the stream; `codeaf logs` printed the log's
path as a first line, so every count was one too many; and `codeaf cache clean` printed its
**question** to stdout, which put the question in the file and left you looking at a blank
terminal waiting for a word you could not see.

`2>/dev/null` silences the commentary and keeps the answer. To keep both separately, redirect
them separately: `codeaf do "…" >answer.txt 2>notes.txt`.

**A command with nothing to show says so in one short sentence, and never prints a column
header with no row under it.** `the cache is empty · <path>`, `nothing is being kept
running.`, `nothing was tried on its own account today.` Silence and a bare header both read
as a command that broke.

## Which of these cost money, and which need no API key

**These read, need no key and spend nothing**: `why`, `telemetry`, `notebook`, `competence`, `services`
(listing), `doctor`, `logs`, `cache`, `show`, `manual`, `version` and `--help`. They are
safe in a shell prompt, a CI step or a bug report.

`models` reads the measured ratings on this machine and also reaches the network for the
model catalog, but spends nothing of yours.

**These spend**, because all of them call a model: `chat`, `do`, `exec`, `run`,
`plan new`, `plan revise`, `plan run` and `wake`. The door opens when the default
service has a key or any connected service holds its credential — a Codex sign-in, an
Ollama connection, a vendor key. So with no OpenRouter key a headless command still
starts once another service is connected; point it at that service's model with
`--model` or the profile's `model.talk`, because a call to a service that has no
credential still fails when it is made, with `no API key: this session has not been
given one yet`. With no credential anywhere, each fails at the door with the same two
lines:

```
codeaf needs a model to work with.
export OPENROUTER_API_KEY (or OPENAI_API_KEY) and run it again.
```

**For the default service, `OPENROUTER_API_KEY` is not required** — it is the first of three places its key is looked
for. The variable, then `OPENAI_API_KEY`, then the default-service key kept in your profile, which is where
the one you pasted on the first run or typed into `/settings` lives. Any one of them is
enough, so a machine set up in the chat runs `codeaf do` with no variable set at all.
Each directly connected service may instead name its own environment variable, which is
stored with that service. `codeaf doctor`'s first row still reports only which default-service key answered — `key set · OPENROUTER_API_KEY`, or
`key set · /home/you/.codeaf/config.json`, or `key none ·` and the two lines above.

**These change state without model spending**: `connect`, `disconnect`, `cache clean`,
`rebuild`, `notebook retract|restore`, `services stop` and `devices revoke`. A browser
connection may make authentication and model-list network requests, but sends no prompt.
The two that destroy something ask
first — `cache clean` wants the word `now` typed out, the same word `/cache clean now`
wants in the chat, and `rebuild` wants `y` — and `--yes` skips the question on both. The other three act at once, and all three can be undone: a
retracted belief restores, a stopped service starts again, a revoked device pairs again.

## What does it count about a run — the anonymous usage counts, and `codeaf telemetry`

`codeaf telemetry` is the door onto the anonymous usage counts: `status` says whether
they are on and why not when they are off; `info` opens on `codeaf does NOT collect or
share your chat` and then says what is collected, shaped like the data — for each stream, under a line naming where it goes or why it is not sent, every
every-event field with the value this machine would send now and one example row per
event (`session_ended  mode=chat  duration=5-30m  turns=6-20 …`) with the stop reasons a
row can carry, and for the Model Pool one example row in the relay's own bytes; `show`
prints what is waiting to leave right now as one JSON object, a key per destination
(`usage`, `model_pool`) with its `destination`, an `off` reason when nothing is sent
there, and `waiting`, the rows themselves, `[]` on the day you install; and `off` and
`on` write the answer to your profile. `info` lists only what is sent, never a disclaimer.
`session_ended` also carries `total_tokens`, the one exact number on it: the input and
output tokens the provider reported across the session, never which model or what it read.
`CODEAF_TELEMETRY=off` — or `DO_NOT_TRACK=1`, or `codeaf telemetry off` — stops both: the
usage counts go quiet and the Model Pool is capped at `read`, so it still picks models
from the index and sends nothing. The pool's own switch, `model_pool` in `/settings` or
`CODEAF_MODEL_POOL`, adds `off`, which asks no judge at all. It reads and
sends nothing of its own — it is a command about the counts, not a session. The
notice names the bargain before the first byte leaves. A chat shows it once, dim,
on the first conversation's screen under the starting points, and nothing is sent
until a frame has drawn it. A task command and `chat --once` print it to stderr
instead. `CODEAF_TELEMETRY=off` or `DO_NOT_TRACK=1` turns the counts off entirely. See
docs/TELEMETRY.md for the whole contract.

## Reading a plan by hand — codeaf plan new, show, revise and run

`codeaf plan` writes a plan to a file you can read, edit and diff, then runs it exactly as
written. It is a real feature and it is not what most people want, because nothing codeaf
learns mid-flight can change a plan that is already frozen.

```
codeaf plan new "<goal>" [--out plan.json] [--dir dir] [--json] [--instructions]
                         [--passes auto|off|N] [--model slug] [--plan-model slug]
codeaf plan show <plan.json>
codeaf plan revise <plan.json> "<what happened>" [--done 1,2,3] [--out plan.json]
codeaf plan run <plan.json> [--dir dir] [--parallel 8] [--out done.json] [--yes-spend]
                            [--max-turns N] [--token-budget N] [--total-token-budget N]
                            [--no-method]
```

`codeaf plan show` is the reader. It takes a plan file and prints `goal:` and then the plan
as a table: the commitments it settled on, the stages, one row per step with what it waits
for, and the instructions under them. It reads a file and nothing else, so it needs no key
and spends nothing. With no file it says `usage: codeaf plan show <plan.json>`.

`codeaf plan revise` takes the same file and an account of what happened, and writes back a
plan changed in the light of it; `--done` names the steps that already landed.

`codeaf plan run` executes the file. `--parallel` is how many steps run at once,
`--max-turns` a backstop per step, `--token-budget` a token wall per step and
`--total-token-budget` one for the whole run, and `--no-method` skips writing a working
method for each step before it runs.

This is a different thing from `codeaf run <program>`, which runs a saved program on typed
input. The two used to share the word `run` and share nothing else.

## The old spellings — what happened to plan, show, revise and run

**`run` used to mean two unrelated commands.** `run graph.json` executed a static plan and
`run subharness <name>` ran a saved program. It means the saved program now, matching
`/subharness <name>` in the chat, and the pipeline moved under the one noun its four verbs
all act on:

| what you used to type | what it is called now |
| --- | --- |
| `codeaf run subharness <name>` | `codeaf run <name>` |
| `codeaf run <plan.json>` | `codeaf plan run <plan.json>` |
| `codeaf plan "<goal>"` | `codeaf plan new "<goal>"` |
| `codeaf show <plan.json>` | `codeaf plan show <plan.json>` |
| `codeaf revise <plan.json> "…"` | `codeaf plan revise <plan.json> "…"` |

**Every old spelling still works for one release.** It is absent from `--help`, it does
exactly what it always did, and it prints one line on stderr the first time it is used:

```
note: `codeaf run subharness <name>` is now `codeaf run <name>` — the old spelling works for one more release.
```

**That line is on stderr and never on stdout**, so `run <name> --json | jq` keeps parsing.
The two are told apart by the SHAPE of what you named, never by the directory you stand in:
a first argument spelled as a path is the old pipeline spelling, and a bare word is a
program. A separator anywhere in it, a leading `./`, `../` or `~`, or a file extension on
the end: any of those is a path, and where both readings would work the path wins, because
that is the one you spelled on purpose. `run formatter` is the saved program from every
folder; `run ./formatter`, `run plans/formatter` and `run plan.json` are plan files from
every folder; a saved program whose name carries a dot, `tidy.up`, is read as a file,
because saved-program names are bare words. It used to be decided by whether the file
existed, which made one command mean two things in two folders.

## Which flags moved — budget, turns, brief, contracts, ensemble

Some flag names moved in the same change, for the same reason: one concept, one spelling,
on every command.

| what you used to type | what it is called now | why |
| --- | --- | --- |
| `--budget N` | `--token-budget N` | *budget* is a word about **money** everywhere else here — `CODEAF_DAILY_BUDGET`, `/budget`, `--max-cost` — so `--budget 150000` read as $150,000 |
| `--run-budget N` | `--total-token-budget N` | the same, for the whole-run wall |
| `--turns N` | `--max-turns N` | it is a limit, and every other limit says so |
| `--max-seconds N` | `--timeout 2m` | one duration flag, one spelling, on `do`, `exec`, `plan run` and `wake` |
| `--brief` | `--instructions` | it writes a self-contained instruction for every step |
| `--contracts=false` | `--no-method` | a boolean that defaults on needs a negative spelling, and what it turns off is a **working method** |
| `--ensemble 0\|-1\|N` | `--passes auto\|off\|N` | a tri-state is words, not magic integers |
| `-w`, `-o`, `-j` | `--dir`, `--out`, `--parallel` | the single letters are shorthands and **keep working forever**, silently; the long names are what is printed |

Each renamed flag says the same one line on stderr the first time it is used, and each old
spelling goes away after one release. `--plan-model` on `exec` is the odd one: that command
plans nothing, so the flag is still accepted and now says
`note: exec does not plan — --plan-model has no effect here.` rather than quietly doing
nothing.

**A flag that does not exist, and a number that will not read, are both refused in this
surface's own words** — spelled with the two dashes you typed, never the one dash Go's flag
package writes:

```
error: codeaf do has no --nosuchflag flag
error: invalid value "notanumber" for flag --max-turns: a whole number of turns to allow, such as 200
```

`--max-turns`, `--turns`, `--token-budget`, `--budget` and `logs --tail` all answer that way;
a negative count is refused too. The command's own usage follows the line, and the exit is 1
— nothing was attempted.

## Is this install healthy — codeaf doctor, and where it keeps things

`codeaf doctor` is the page to open when nothing works. It reads this machine and prints a
short block of labelled rows. It needs no key and spends nothing.

```
codeaf doctor [--db path]
```

Every row is labelled in the words a developer would search for:

```
store            /home/you/.codeaf/graph.db · 496 KiB
resident         this terminal while open
background timer not installed · last wake not yet
spend            rail $20.00
model calls      /home/you/.codeaf/logs/calls.jsonl · 26 KiB
```

`store` names the file the journal and every derived table live in, and how big it is —
`· not created` on a machine that has not made one yet. `background timer` is the row about
the five-minute pass: whether it is installed, when it last woke, when it next checks, and
`· checks look stalled` when an installed one has not woken for several cadences. `spend`
is the day against its limit, and `model calls` is where the call log is and what it
weighs. A machine with charters or unanswered questions on it prints a `standing` row too.

Both of the first two labels are recent. `store` used to read `brain` — nobody looking for
where their data lives searches for a brain — and `background timer` used to be named after
a piece of the *other* product in this binary, which is not a thing the chat has at all.

**It leaves out what it has not measured.** A machine that has spent nothing today prints
the limit and no figure beside it, rather than `$0.00` — a zero nobody measured reads as a
machine that counted, which is the opposite of the truth. Same for the counts beside it:
nothing to say is nothing printed.

That command is about this *install*. `/status` in the chat is about the conversation you
are in. They are related and they are not the same reading.

## codeaf help, and the verbs that take no flags at all

`codeaf help` is a third spelling of `codeaf --help`, beside `-h`, and all three print the
same list of commands. Every verb answers for itself the same way — `<verb> --help`, on
stdout, exiting **0**, because asking for help is not a failure — and the fuller account of
that, and of what a mistyped command is answered with, is on the *commands* page.

Two things that account does not cover:

- **The doors that parse no flags at all answer the gesture too.** `codeaf plan show` and
  `codeaf cache` take a positional or nothing, and each reads `--help` as the question
  rather than as an argument. (`codeaf models` has one flag, `--refresh`, which fetches
  today's model list first — what `ctrl+r` does in `/model`.) `codeaf plan show --help` used to answer
  `open --help: no such file or directory` — a filesystem error about a flag.
- **`help env` is the environment table.** It moved off `--help` when that page was 127
  lines and more than half of them were this table, so the last thing on the screen after
  asking what the commands are was `CODEAF_CALL_LOG_BODIES`.
- **`codeaf manual --help` prints its usage, then the list of pages.** The list is what
  that command can be asked for, so it is still there; it used to be *all* that was there,
  which made one verb in the binary answer `--help` differently from the other twenty-two.

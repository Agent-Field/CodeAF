# Telemetry

codeaf counts how it is used — how often, in which modes, on which platforms —
so the parts people rely on get the work. The counts are anonymous: nothing
about you or your work ever leaves this machine.

## The notice

Before the first session's events are sent, codeaf shows this once:

```
codeaf sends anonymous usage counts to AgentField.
  Sent:  version, OS, mode, session counts, errors, and total tokens used.
  Never: anything about you or your work. No prompts, code, file names,
         paths, repo names, keys, email, IP, or machine name.
  What is collected:        codeaf telemetry info
  Turn off:                 CODEAF_TELEMETRY=off
```

The installer prints nothing about telemetry; the notice above arrives with the
first session, before anything is sent. A chat shows it on the first
conversation's screen, dim, under the starting points, and it counts as shown
only once a frame has drawn it: a window too short to hold all six lines, or the
first-run setup standing in front, leaves it owed for the next launch. A task
command (`do`, `run`, `plan run`) and `chat --once` draw no screen, so they print
it to stderr before they start. Until it has been shown, nothing is sent.

## What is sent

Exactly four events. Each carries the every-event properties; three of them
add more. Values are counts, bands, or words from fixed lists. The
table's names come from the same allowlist the code is held to and its words
from the table `codeaf telemetry info` prints, and a test fails the build if
any of the three drift apart.

| Event | Property | What it is |
| --- | --- | --- |
| every event | codeaf_version | the release tag this binary was built from, at most 64 characters |
| every event | channel | stable, rc, staging, dev, or unknown |
| every event | os | darwin, linux, windows, or other |
| every event | arch | amd64, arm64, or other |
| every event | usage_context | local (a person's machine), ci, or container |
| every event | install_method | script, source, or unknown |
| session_started | mode | chat or task |
| session_started | resumed | whether the session continued an earlier one |
| session_ended | mode | chat or task |
| session_ended | duration | a band: under 1m, 1-5m, 5-30m, 30m-2h, 2h or more |
| session_ended | turns | a count band |
| session_ended | model_calls | a count band |
| session_ended | model_calls_failed | a count band |
| session_ended | tool_calls | a count band |
| session_ended | tool_calls_failed | a count band |
| session_ended | cost_usd | a dollar band |
| session_ended | total_tokens | total provider-reported input and output tokens in this session |
| session_ended | stop_reason | done, error, incomplete, budget, turn-cap, deadline, price, question, interrupted, or unknown |
| session_ended | exit_code | 0 to 5 |
| fault | mode | chat, task, or other |
| fault | scope | main, goroutine, or surface |
| fault | fingerprint | 16 hex characters hashed from codeaf function names in the stack |

Count bands are 0, 1, 2-5, 6-20, 21-100 and 100+. Dollar bands are 0, under
0.01, 0.01-0.1, 0.1-1, 1-10 and 10+.
The numeric `total_tokens` count sums provider-reported input and output across
the run, including tokens read from cache once. Missing usage contributes zero.

first_run carries only the every-event properties and is sent once per
install.

Each event also carries its identity as hashes: a random per-event id, sha256
of the install id, and — except on first_run — sha256 of the run id. The raw
ids never leave this machine.

## What is never sent

Prompts, model replies, code, file names, paths, repo or directory names, git
remotes, hostnames, usernames, IP addresses, environment values, API keys,
email addresses, model names, error text, panic messages. A test builds every
event from inputs stuffed with exactly these and fails if any of them reach
the marshalled output.

## Where events wait

Events wait in ~/.codeaf/telemetry/spool.jsonl until they are sent: at most 50
per request, nothing older than 7 days, at most 1000 lines kept, and nothing
sent before the notice has been shown. An event whose version is unknown is
dropped at send time and never leaves the machine. `codeaf telemetry show`
prints exactly what has not left yet, as JSON.

## Turning it off

Any one of these turns the counts off, and every one of them also stops the
Model Pool from sending. They are checked in this order:

1. `CODEAF_TELEMETRY=off` — also `0` or `false`.
2. `DO_NOT_TRACK=1` — also `true`, the ecosystem's own word for it.
3. `telemetry = off` in the project's settings file, `.codeaf/config.json`. A
   project may only turn the counts off, never on.
4. `codeaf telemetry off`, which writes the profile setting; `codeaf telemetry
   on` is the way back.
5. an empty `CODEAF_TELEMETRY_ENDPOINT`.

A build that cannot name its own source — dirty or unstamped — never reports,
and neither does a test binary.

## The Model Pool is a second stream, under its own switch

The usage counts are not the only thing this binary sends to AgentField. With
`model_pool` set to `on` — the default — a judge scores each crew seat after a
task lands, and one row per seat leaves for
`https://codeaf.agentfield.ai/pool/v1/rows`: the model slug that held the
seat, the judge's slug, the seat (the worker, checker or planner, spelled on the wire as `worker`,
`high` and `mastermind`), a 0-100 score,
the door the run came in by (task, do, exec or run), the crew size and the UTC
day, under a random per-install nonce in an `X-Codeaf-Install` header. No prompt, code, path or name rides in a row. **Every way of turning the
counts off turns this stream off too** — `CODEAF_TELEMETRY=off`,
`DO_NOT_TRACK=1`, the project file, `codeaf telemetry off` — by capping the
pool at `read`: the index is still read and the judge still scores into the
install's own sheet, but nothing is sent, and `codeaf pool status` says `mode
read · telemetry`. That cap wins over an explicit `model_pool = on`, because
the notice's "Turn off" line carries no exception. The pool's own switch,
`model_pool` in settings or `CODEAF_MODEL_POOL`, adds `off` (ask no judge at
all). `codeaf telemetry info` describes both streams and `codeaf telemetry
show` prints the rows waiting to leave from both, so "what is collected" is
answered for everything the binary sends.

## The command

`codeaf telemetry` reads the counts and never sends anything of its own.

- `codeaf telemetry status` says whether the counts are on, and why not when
  they are off.
- `codeaf telemetry info` opens on the fact a person came to check — `codeaf
  does NOT collect or share your chat`, with the never list — then prints what
  is collected, from BOTH streams, shaped like the data: for the usage counts,
  every every-event field with the value
  this machine would send now, one example row per event (`mode=chat
  duration=5-30m  turns=6-20 …`, from the contract's own bands) and the stop
  reasons a row can carry; for the Model Pool, one example row in the bytes
  the relay receives and the two identities a batch travels under. Each stream
  sits under a numbered heading, `1. Usage Counts` and `2. Model Pool`, naming
  where it goes or why it is not sent. It lists only what is sent — the never lists are the notice's and this page's.
- `codeaf telemetry show` prints what is waiting to leave right now, as one
  JSON object indented by two: a key per destination, `usage` and
  `model_pool`, and under each its `destination`, an `off` reason when
  nothing is sent there, and `waiting`, the rows in the bytes the relay would
  receive, `[]` when none wait — which is what a fresh install shows.
- `codeaf telemetry off` and `codeaf telemetry on` write the profile setting.

```
telemetry on
  endpoint: https://agentfield.ai/api/oss/codeaf/telemetry
  notice: shown
  spooled events: 3
  install: 4f2a9c1b7e08…
```

When the counts are off, a `reason:` line follows `telemetry off` and names the
switch that turned them off.

## Download counts

The download numbers this repository reports come from public GitHub release
data: the GitHub API publishes a cumulative download count for every asset on
a release, and a daily scheduled workflow reads those counts and sends one
event per binary asset to PostHog (`codeaf:release_downloads`). No code in
the binary is involved, nothing is collected from the person downloading, and
the only facts in the event are the release tag, the asset's platform, and
the count GitHub already shows on the release page. Sidecar files such as
`checksums.txt` are not counted. The workflow lives in
`.github/workflows/release-downloads.yml`; the script behind it is
`scripts/release_downloads.py`, and running it with `--dry-run` prints the
batch it would send.

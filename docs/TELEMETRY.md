# Telemetry

codeaf counts how it is used — how often, in which modes, on which platforms —
so the parts people rely on get the work. The counts are anonymous: nothing
about you or your work ever leaves this machine.

The counts are **ON by default**. They go to AgentField's relay at
`https://agentfield.ai/api/oss/codeaf/telemetry` (`telemetry.DefaultEndpoint`).
`CODEAF_TELEMETRY_ENDPOINT` moves the relay; an empty value turns sending off.

## The disclosure

This page, and the *Telemetry* section of the repository's README, are the whole
of it. In one paragraph:

> codeaf sends anonymous usage counts to AgentField. Sent: version, OS, mode,
> session counts, errors, and total tokens used, as counts and bands. Never:
> anything about you or your work — no prompts, code, file names, paths, repo
> names, keys, email, IP, machine name, or model names. Turn off: the `telemetry`
> switch in `/settings`, `CODEAF_TELEMETRY=off`, or `DO_NOT_TRACK=1`.

**The product itself says nothing about it.** Until 2026-10-01 codeaf printed a
six-line notice once per install — on the first conversation's screen, or to
stderr ahead of a task — and sent nothing until that notice had been shown; it
also carried a `codeaf telemetry` command (`status`, `info`, `show`, `on`, `off`)
that printed the fields and the waiting rows. Both were removed: new people read
the notice as something to deal with on a screen that is supposed to be three
sentences long, and a disclosure belongs in the repository, where it can be read
before anything is installed. The installer prints nothing about telemetry
either. The counts are sent from the first session on, under the switches below.

## What is sent

Exactly five events. Each carries the every-event properties; four of them
add more. Values are counts, bands, or words from fixed lists. The
table's names come from the same allowlist the code is held to and its words
from the same table the code describes each field with, and a test fails the build if
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
| usage_delta | mode | chat or task |
| usage_delta | input_tokens | provider-reported input tokens since the preceding usage event |
| usage_delta | output_tokens | provider-reported output tokens since the preceding usage event |
| usage_delta | total_tokens | the sum of this event's input and output tokens |
| session_ended | mode | chat or task |
| session_ended | duration | a band: under 1m, 1-5m, 5-30m, 30m-2h, 2h or more |
| session_ended | turns | a count band |
| session_ended | model_calls | a count band |
| session_ended | model_calls_failed | a count band |
| session_ended | tool_calls | a count band |
| session_ended | tool_calls_failed | a count band |
| session_ended | cost_usd | a dollar band |
| session_ended | stop_reason | done, error, incomplete, budget, turn-cap, deadline, price, question, interrupted, or unknown |
| session_ended | exit_code | 0 to 5 |
| fault | mode | chat, task, or other |
| fault | scope | main, goroutine, or surface |
| fault | fingerprint | 16 hex characters hashed from codeaf function names in the stack |

Count bands are 0, 1, 2-5, 6-20, 21-100 and 100+. Dollar bands are 0, under
0.01, 0.01-0.1, 0.1-1, 1-10 and 10+.
Each numeric usage delta is written when provider accounting arrives, including
tokens read from cache once. Missing usage contributes no event. Deltas, rather
than repeated session totals, make the stream safe to sum while a run is open.

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
per request, every 30 seconds while a session is open and once more when it
ends. Nothing older than 7 days is sent and at most 1000 lines are kept. An
event whose version is unknown is dropped at send time and never leaves the
machine. The spool is plain JSON lines, so what has not left yet can be read
with any editor.

## Turning it off

Any one of these turns the counts off, and every one of them also stops the
Model Pool from sending. They are checked in this order:

1. `CODEAF_TELEMETRY=off` — also `0` or `false`.
2. `DO_NOT_TRACK=1` — also `true`, the ecosystem's own word for it.
3. `telemetry = off` in the project's settings file, `.codeaf/config.json`. A
   project may only turn the counts off, never on.
4. the `telemetry` switch on the *display* tab of the chat's `/settings`, which
   writes `telemetry = off` to the profile's `config.json` and stops sending
   immediately, including later periodic and exit flushes. Turning it back on
   takes effect the next time codeaf starts. A request already on the wire may
   complete; counts already queued locally stay unsent while it is off.
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
`DO_NOT_TRACK=1`, the project file, the `telemetry` switch in `/settings` — by
capping the pool at `read`: the index is still read and the judge still scores
into the install's own sheet, but nothing is sent, and `codeaf pool status`
says `mode read · telemetry`. That cap wins over an explicit `model_pool = on`,
because the disclosure's "Turn off" line carries no exception. The pool's own
switch, `model_pool` in settings or `CODEAF_MODEL_POOL`, adds `off` (ask no
judge at all). `codeaf pool status` says how many rows wait to leave for the
pool; this page describes both streams, so "what is collected" is answered
for everything the binary sends.

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

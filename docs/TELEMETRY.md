# Telemetry

codeaf counts how it is used — how often, in which modes, on which platforms —
so the parts people rely on get the work. The counts are anonymous: nothing
about you or your work ever leaves this machine.

## The notice

Before the first session's events are sent, codeaf prints this to stderr once;
the installer prints it too:

```
codeaf sends anonymous usage counts to AgentField.
  Sent:  version, OS, mode (chat or task), how many sessions, how many errors.
  Never: anything about you or your work. No prompts, code, file names,
         paths, repo names, keys, email, IP, or machine name.
  See exactly what leaves:  codeaf telemetry show
  Turn off:                 CODEAF_TELEMETRY=off
```

## What is sent

Exactly four events. Each carries the every-event properties; three of them
add more. Every value is a count, a band, or a word from a fixed list. The
table is generated from the same allowlist the code is held to, and a test
fails the build if the two ever drift apart.

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
| session_ended | stop_reason | done, error, incomplete, budget, turn-cap, deadline, price, question, interrupted, or unknown |
| session_ended | exit_code | 0 to 5 |
| fault | mode | chat, task, or other |
| fault | scope | main, goroutine, or surface |
| fault | fingerprint | 16 hex characters hashed from codeaf function names in the stack |

Count bands are 0, 1, 2-5, 6-20, 21-100 and 100+. Dollar bands are 0, under
0.01, 0.01-0.1, 0.1-1, 1-10 and 10+.

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
sent before the notice has been shown. `codeaf telemetry show` prints exactly
what has not left yet.

## How to turn it off

Any one of these, before codeaf starts:

- `CODEAF_TELEMETRY=off` (also `0` or `false`)
- `DO_NOT_TRACK=1` (also `true`)
- the config key `telemetry = off`
- an empty `CODEAF_TELEMETRY_ENDPOINT`

A build that cannot name its own source — dirty or unstamped — never reports,
and neither does a test binary.

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

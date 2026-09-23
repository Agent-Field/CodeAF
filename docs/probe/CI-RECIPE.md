# CI recipe: a repeatable codeaf-probe journey

A CI journey is a small script that drives `bin/codeaf-probe` against a
prepared fixture, asserts on **real rendered screen state**, records the
outcome into the session's evidence file, and exits non-zero when an
assertion fails. Nothing here stands in for live-agent verification; see
["What a stub proves vs what live-agent verification proves"](#what-a-stub-proves-vs-what-live-agent-verification-proves).

## The journey contract

- Every CLI invocation prints exactly one compact JSON envelope on stdout.
- Every act/observe is appended, step-indexed, to
  `<probe root>/recordings/<session>.jsonl` (private perms: 0600, dir 0700).
- A journey that fails **must** record the failure:
  `codeaf-probe record-outcome --session <id> --outcome failed --reason "..."`,
  then `exit 1`. A failure never silently passes; the evidence file says so.
- Recordings are **evidence, not re-execution**: a recording proves what was
  observed when it ran. Do not replay it against a live session and treat the
  result as verification — a live CodeAF session is nondeterministic. Playback
  reads the file; it does not drive the terminal again.

## Make-style example

```make
PROBE := ./bin/codeaf-probe
JOURNEY := scripts/probe-journey.sh

.PHONY: probe-prereqs probe-journey
probe-prereqs:
	make build            # writes bin/codeaf (the real binary under test)

probe-journey: probe-prereqs
	$(JOURNEY)            # exits non-zero on any assertion failure
```

```sh
#!/bin/sh
# scripts/probe-journey.sh — one repeatable journey.
set -u
export CODEAF_PROBE_BASE="$PWD/.probe-ci"   # isolated: never the user's data
P="$PWD/bin/codeaf-probe"
B="$PWD/bin/codeaf"
SESSION="ci-$$"

cleanup() { $P finish --session "$SESSION" >/dev/null 2>&1 || true; }
trap cleanup EXIT

fail() { $P record-outcome --session "$SESSION" --outcome failed --reason "$1"; exit 1; }

# 1. Pin the exact binary under test; identity is recorded.
$P prepare --bin "$B" >/dev/null || { echo "prepare failed"; exit 1; }

# 2. Reset a versioned fixture: reproducible product state, schema-aware,
#    isolated from any real user's home.
$P fixture-reset --scenario clean >/dev/null || exit 1

# 3. Start a persistent session over the REAL binary in a real terminal.
$P start --session "$SESSION" --profile reviewer --bin "$B" >/dev/null || exit 1

# 4. Act and observe through real text/keys — never internal shortcuts.
$P act --session "$SESSION" --text "review the diff in cmd/chatv3" >/dev/null || exit 1
$P act --session "$SESSION" --keys Enter >/dev/null || exit 1

# 5. Assert on rendered screen state.
$P observe --session "$SESSION" | grep -q "the-thing-that-must-appear" \
  || fail "assertion failed: expected marker not on screen"

# 6. Honest completion.
$P record-outcome --session "$SESSION" --outcome ok --reason "journey completed"
```

Run it repeatedly; it is deterministic about its fixtures and honest about
whatever the screen shows. On failure CI sees a non-zero exit **and** the
session's recording ends with `{"verb":"outcome","outcome":"failed","reason":…}`.

## Multiple scenarios

`fixture-reset --scenario <name>` is idempotent and version-aware (a recorded
manifest pins the fixture's contents). Give each scenario its own session id
and its own `fixture-reset`, and run scenarios as separate CI jobs:

```sh
for sc in clean returning-user; do
  $P fixture-reset --scenario "$sc" >/dev/null || exit 1
  $P start --session "ci-$sc" --profile reviewer --bin "$B" >/dev/null || exit 1
  # ... assertions per scenario ...
done
```

## What a stub proves vs what live-agent verification proves

| | A stub (a scripted stand-in binary, as in the Go tests) | Live-agent verification (real CodeAF, real model) |
| --- | --- | --- |
| Proves | the probe contract itself: session persistence across CLI calls, rendered snapshots, stale-revision rejection, recording correspondence, fixture reset repeatability, cleanup of owned resources | what a real CodeAF turn actually renders, how the UI behaves under real model latency, whether an agent can complete a real task |
| Cannot prove | anything about CodeAF's actual behavior — the stub echoes whatever the test types | nothing about the probe layer itself beyond "it worked this once" |
| Cost | free, deterministic, runs in CI minutes | paid model calls, minutes per turn, nondeterministic |

Both have a place: the stub suite (`go test ./internal/probe
./cmd/codeaf-probe`) gates every pull request; a live journey is a soak/campaign
activity, not a per-commit gate.

## Reusing the hosted overlapping-task/draft-preservation journey

`scripts/hosted-drive.sh` runs real CodeAF chat in an isolated home, workspace
and tmux server with actual model calls, and checks overlapping task handoffs,
task landing, history and draft return (unsaved work preserved). Its
prerequisites are a working model backend (credentials, spend) and the hosted
environment it drives; it is not fixture-seeded and not idempotent, so it does
not run as-is in routine CI. **Exact limits if live prerequisites are absent:**
without model credentials/spend, that journey cannot run at all — CI then
proves the probe contract on the stub suite plus the fixture-backed journey
above (real binary startup, real terminal interaction, real assertions), and
says so: it does NOT verify live model behavior, task handoff under real
generation, or draft preservation against the real UI. Run `hosted-drive.sh`
where its prerequisites exist and attach its output as campaign evidence.

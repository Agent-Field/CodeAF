# CI recipe: a repeatable codeaf-probe journey

A CI journey drives `bin/codeaf-probe` against the real `bin/codeaf`, asserts
on **real rendered screen state**, records the outcome into the session's
evidence file, and exits non-zero when an assertion fails. The commands below
were executed once against the real binary with their outputs saved under
`probe-evidence/docs/`.

## The journey contract (as implemented)

- Every CLI invocation prints exactly one compact JSON envelope on stdout.
- Steps and observations are appended to the session's evidence file under the
  probe root; `finish` writes the terminal record, and
  `codeaf-probe record-outcome --session <id> --outcome failed --reason "..."`
  records a failure before `exit 1`. A failure never silently passes.
- Recordings are **evidence, not re-execution**. There is no playback verb
  today (deferred); a recording proves what was observed when it ran.

## Journey script (uses the implemented verbs only)

```sh
#!/bin/sh
# probe-journey.sh — one repeatable journey.
set -u
export CODEAF_PROBE_BASE=/tmp/prb-docs.XXXXXX   # short path; isolated base, never the user's data
P="$PWD/bin/codeaf-probe"
B="$PWD/bin/codeaf"
SESSION="ci-$$"

cleanup() { $P finish --session "$SESSION" >/dev/null 2>&1 || true; }
trap cleanup EXIT

fail() { $P record-outcome --session "$SESSION" --outcome failed --reason "$1"; exit 1; }

# 1. Pin the exact binary under test; identity is recorded.
$P prepare --bin "$B" >/dev/null || { echo "prepare failed"; exit 1; }

# 2. Prepare the versioned fixture: reproducible product state, manifest-verified,
#    isolated from any real user's home. (No `fixture verify` verb; prepare
#    self-verifies.)
$P fixture-prepare --scenario clean >/dev/null || exit 1

# 3. Start a persistent session over the REAL binary in a real terminal.
$P start --session "$SESSION" --profile reviewer --bin "$B" >/dev/null || exit 1
sleep 2

# 4. Act and observe through real text/keys — never internal shortcuts.
$P act --session "$SESSION" --keys Enter >/dev/null || exit 1
$P act --session "$SESSION" --resize 100x40 >/dev/null || exit 1

# 5. Assert on rendered screen state.
$P observe --session "$SESSION" | grep -q "codeaf" \
  || fail "assertion failed: expected marker not on screen"

# 6. Honest completion, then owned cleanup.
$P record-outcome --session "$SESSION" --outcome ok --reason "journey completed"
$P finish --session "$SESSION"
```

Run it repeatedly: fixtures are idempotent and manifest-verified, so the same
journey is repeatable; the screen decides, and the recording says how it ended.

## Multiple scenarios

Scenarios today are `clean` and `returning`; give each its own session id and
its own `fixture-prepare`, and run scenarios as separate CI jobs:

```sh
for sc in clean returning; do
  $P fixture-prepare --scenario "$sc" >/dev/null || exit 1
  $P start --session "ci-$sc" --profile reviewer --bin "$B" >/dev/null || exit 1
  # ... assertions per scenario ...
  $P finish --session "ci-$sc"
done
```

## What a stub proves vs what live-agent verification proves

| | A stub (a scripted stand-in binary, as in the Go tests) | Live-agent verification (real CodeAF, real model) |
| --- | --- | --- |
| Proves | the probe contract itself: session persistence across CLI calls, rendered snapshots, stale-revision rejection, fixture repeatability, cleanup of owned resources | what a real CodeAF turn actually renders, how the UI behaves under real model latency, whether an agent can complete a real task |
| Cannot prove | anything about CodeAF's actual behavior — the stub echoes whatever the test types | nothing about the probe layer itself beyond "it worked this once" |
| Cost | free, deterministic, runs in CI minutes | paid model calls, minutes per turn, nondeterministic |

The stub suite (`go test ./internal/probe ./cmd/codeaf-probe`) gates changes;
a live journey is a soak/campaign activity, not a per-commit gate.

## Deferred, and stated plainly

- **Live-model campaigns** (`scripts/hosted-drive.sh`-style real turns): not a
  CI gate. Without model credentials/spend that journey cannot run; CI then
  proves the probe contract on the stub suite plus the fixture-backed journey
  above — it does NOT verify live model behavior, task handoff under real
  generation, or draft preservation against the real UI.
- **Spark SSH cluster**: no documented host/config is wired into this recipe;
  the cluster path is deferred, not assumed.
- `start --fixture`, `fixture verify`, and a `record`/playback verb do not
  exist yet in the CLI (see AGENT-GUIDE "Not implemented today").

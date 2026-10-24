# Deterministic fault gate

This development package runs 3–5 **production** `raftcore` instances on one
virtual-time event queue. It adds no product route, environment switch, socket,
clock read or background consensus loop. P09 owns the authorized production
application adapter; this packet's `ProbeApplication` exercises the real pure
lease rules and a small session-policy fixture. The independent oracle in
`model` uses neither those rules nor production state types. Assignment policy
extends that oracle in P16; it is not advertised as implemented here.

## Run the bounded gate

From the repository root, with no other heavy command running:

```sh
sh scripts/raft-proof-footprint.sh nice -n 5 env GOMEMLIMIT=128MiB \
  /usr/bin/time -v go test -p=1 ./internal/raftsim/... \
  -run '^TestR' -count=1 -timeout=120s -v
```

The wrapper admits work only at I/O `some avg10 <= 40`, uses GOMAXPROCS=2,
GOFLAGS=-buildvcs=false, one compiler worker and effective nice 15. The campaign
uses seeds 0–499 serially, at most 2,000 processed events per seed, 2 cells and
fewer than 100 operations. One whole-identity Porcupine check gets at most one
second; timeout/Unknown fails the proof. No checker partition hides cross-cell
vault/revocation/session dependencies. No HTML or large fixture is generated.
Supplementary bounded cases cover all 16 surviving joint-majority subsets,
every enter/leave Ready boundary, 100 minority flaps, every snapshot chunk and
install boundary, both retained generations and native SQLite parity.

Set `RAFTSIM_SEEDS=<n>` to run seeds 0 through n−1 in both the fault campaign
and the election-timeout campaign. Unset means 500; empty, zero, negative and
noninteger values fail. For the larger serial soak, use the same footprint:

```sh
sh scripts/raft-proof-footprint.sh nice -n 5 env GOMEMLIMIT=128MiB RAFTSIM_SEEDS=5000 \
  /usr/bin/time -v go test -p=1 ./internal/raftsim/... \
  -run '^TestR' -count=1 -timeout=180s -v
```

The 6–12 tick election timeout bounds one attempt. Split votes and omitted
messages can require several attempts; the campaign allows eight maximum
timeout windows while retaining the 2,000-event ceiling. A separate seeded
check asserts one delivered tick per live node per round and each election
attempt within 6–12 ticks. This budget is not a phone-fallback latency claim.
Lost-reply retries first wait for the original request's durable session result
without submitting a second proposal. An absent result after a timeout remains
unknown; it cannot be compared to a later successful retry as an empty result.
Portable replay fixtures retain the original split-vote and uncommitted-timeout
prefixes that exposed these two harness mistakes.

To keep exact negative counterexamples, pass the simulator package alone:

```sh
sh scripts/raft-proof-footprint.sh nice -n 5 env GOMEMLIMIT=128MiB \
  go test -p=1 ./internal/raftsim -run '^TestR1R3R4Broken' -count=1 \
  -raftsim-evidence-dir=/tmp/codeaf-raftsim-evidence
```

The evidence flag belongs only to that test binary. Do not pass it to the
separate `model` package. `testdata` contains independently specified good and
bad histories; broken fence/read/flush variants must be detected by the same
checker/ordering monitor used for healthy runs.

## Drive and replay

`New(Config)` creates three identically bootstrapped voters and optional empty
members 4–5. `Do(Action)` records every external command/fault. `Drain` processes
bounded queued work; `Ticks` advances explicit election ticks. A proposal's
admission is never a successful operation. `History` records invocation and
response order; a timeout stays as an unknown outcome and may commit later.
Cancelled reads use the actual core cancellation input. Reads complete only
from quorum ReadIndex and durable application state.

`Evidence()` includes seed/config, ordered actions, event trace, separate flush
trace, whole history, checker result and minimized illegal history. `Run(replay)`
reconstructs all RNG seeds, deliveries, effects and replies; `Digest` compares
the exact trace and observations. `WriteEvidence(dir)` is explicit host I/O for
synthetic fixtures, outside the scheduler. Tests export this bundle on failure;
negative controls export it when the evidence flag is supplied.

Network faults include omission, duplicates, seeded delay/reorder, directed
partitions, held read acknowledgments and temporary stalls. Recovery gets a new
recorded seed and handle generation. Old delivery/completion callbacks cannot
complete work on that handle. Persist-write, flush, completion, apply-write,
application flush, completion and reply are distinct events. Power loss keeps
only the flushed image and discards staged/torn bytes; process death may retain
a complete unflushed transaction. Empty Ready barriers have no physical write.
Disk errors stop participation. Every dependent send/application/reply is checked
against its flush, and every state transition checks leader-per-term, committed
log agreement, monotonic indexes/fences and application equivalence.

The disk model is a fault model, not another durable storage implementation.
P04/P05 prove SQLite process cuts and actual WAL flushes. The native parity test
replays the simulator's exact production batches into real SQLite stores and
compares recovery bytes, application state and portable snapshots; simulated
snapshot chunks are also installed through the real store's API. The simulated
one-row application stays below the portable 1 MiB row envelope; it is not the
paged production directory evaluator or a migration representation.

## Constraints subsequent packets must keep

Create a new checkpoint **after committed learner admission** when catch-up
requires a compacted prefix. etcd refuses an older snapshot whose configuration
omits the recipient. Wait until the learner applies the advertised barrier
before proposing its promotion. This test host supplies the mechanism; P12 owns
the durable enrollment/intent and policy workflow.

Snapshot transfer completion is separate from Raft installation. Chunks stage
without changing application authority, complete streams are checksummed,
incomplete generations are ignored on recovery, and corrupt complete bytes
refuse recovery. Retain two complete generations. Report transport success only
after the real snapshot Ready is installed durably; report interruption/timeout
so RawNode can retry. A transport receipt alone is never an append acknowledgment.

The fixture session contract keeps one exact result per live session, requires
an acknowledgment to advance, retains closed-ID tombstones, refuses conflicting
digests and caps live sessions at 128. Acks/close are replay-catalog inputs until
P09 supplies their real schema; no public protocol for them is invented here.
Authentication, durable upload receipts, complete directory verbs, assignments,
learner quotas/transport queues, hosted actors and client journeys remain the
packets that own them. R1's displayed sentence, unsent local branches and the
other X/J requirements are not claimed by a metadata simulator.

Porcupine v1.1.0 is MIT licensed and only imported by the simulator checker.
Neither this dependency nor the simulator is linked into `bin/codeaf`.


P12 extends the bounded topology to three voters plus at most 32 learner cores.
Its full-fanout case admits the learners in one internal configuration batch,
sleeps all of them, commits writes, compacts, restores a lagging learner through
a fresh snapshot and removes it with delayed responses. The unchanged 2,000-event
ceiling still applies. Separate signed-policy tests prove sequential admission,
its 33rd-learner refusal, revision races and retained ID tombstones; the raw
simulator configuration action is not a public admission policy.

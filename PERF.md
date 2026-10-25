# Measuring performance

## Measure the changed path

Use focused tests and benchmarks while editing. Record the Go version, operating
system, architecture, benchmark command and relevant fixture size so results can
be reproduced. Compare the same workload before and after the change.

```sh
make test-focus PKGS=./internal/pkg RUN='^TestRegression$$'
go test ./internal/pkg -run '^$' -bench BenchmarkName -benchmem -count=5
make test-report PKGS=./internal/pkg REPORT=/tmp/codeaf-tests.json
```

Replace the package and test names with the path being measured. Benchmark timing
is diagnostic; regression gates count deterministic work, bytes, allocations,
request fanout, retained data and cache reuse. Do not gate on wall-clock thresholds.
Use scripted providers and explicit synchronization rather than real sleeps.

Activity layout lines share immutable task records instead of copying both task
and conversation payloads into every line. Conversation overlays keep their own
copy, so live titles do not mutate the held reading. Paint scratch still expires
before the next input. `TestActivityScalePaintAllocationBudget` exercises the
actual body with 5,120 tasks and enforces a 4 MiB allocation budget per paint;
`BenchmarkActivityScaleFrame` and `TestActivityScaleTerminalLatency` measure the
same workload at the body and real-terminal interfaces.

Team disbanding reads reporting homes once through the existing batch reader
before changing memberships. `BenchmarkDisbandScale` covers 10,000 and 40,000
memberships; the batch trades temporary indexed storage for repeated scans.
`TestTeamsDisbandScaleTerminalLatency` measures confirmation through the visible
saved result, including persistence and redraw work.

Store reopening probes distinct nonempty session IDs through the existing
message index before checking for missing session rows. It still scans message
IDs, and still discovers older writers' missing rows on a later open; no cached
migration flag skips that check. `BenchmarkOpenSettledSessions` measures the
whole public open/close operation. `TestSettledStoreTerminalStartup` separates
fixture construction from the first interactive frame of the running terminal.

## Live tool approval settings

Tool decisions refresh through the existing approval gate. The config readers
reuse parsed files until metadata or the settings generation changes; the gate
holds one compiled policy and reuses it while its three resolved rows match.
There is no watcher, polling loop or per-call policy compilation for unchanged
settings. Clarification conversations follow their parent's gate; ordinary task
workers use a fixed allow policy. Versioned refresh publication cannot overwrite
a newer refresh, policy push or conversation posture. The decision snapshot is
copied under the same lock that validates publication.

Factory stages also reuse their fixed allow policy. `TestStageAllowPolicyReuse`
checks policy identity and zero allocations per unchanged build (100 builds per
measurement), down from 10 allocations before caching, on Go 1.26.5,
linux/arm64. Reproduce with:

```sh
make test-focus PKGS=./cmd/codeaf RUN='^TestStageAllowPolicyReuse$'
```

`BenchmarkApprovalGateUnchangedSettings` uses a 101-byte profile containing a
mode, tool exception and shell rule, with no project file. On Go 1.26.5,
linux/arm64, three runs measured 8,264 bytes and 72 allocations per build before
reuse, versus 2,720–2,736 bytes and 29 allocations afterwards. Timing was
17.7–20.2 microseconds before and 8.4–10.0 microseconds afterwards; it is
diagnostic, not a gate. Reproduce with:

```sh
make test-focus PKGS=./cmd/codeaf RUN='^$' TEST_FLAGS='-bench BenchmarkApprovalGateUnchangedSettings -benchmem -count=3'
```

## What the gates check

`make test-quick` checks compilation, vet, formatting, packed manuals, change
entries, manual coverage and structural laws. `make pr-ready` adds fresh tests
for packages selected from the committed diff. CI uses the same touched-package
selector. Heavy suites share a per-machine lock; do not stack full chat or engine
runs during an editing loop. Use `SHARDS=1` when a serial reproduction is needed.

Performance tests beside the implementation pin bounds on prompt and response
sizes, allocations, decompression, request counts, retained previews and rendering
work. Preserve those assertions when changing a hot path. Paint must not perform
filesystem or network reads. Document any cap change with its rationale in the
same commit; derive repeated values from constants.

## Binary size

`SIZE-BUDGET` is the stripped binary's byte cap. `make size` builds through
`make build`, weighs `bin/codeaf` and fails above that cap. `make check` includes
this gate; `make build` alone does not impose it. CI's size report is informational.

Compare builds with the same Go version, target, flags and embedded engine
artifact. The reference target is darwin/amd64 with its own pinned artifact staged;
other platforms can have different absolute sizes. Reduce unnecessary code, data
or dependencies first. A justified cap adjustment belongs in the same change as
the feature and its measurements; keep `SIZE-BUDGET` explicit and reviewable.

## Factory execution capacity

New runners default to **15 concurrent jobs**, with a configurable **1–100**
limit per owner and computer. This is a bounded product policy, not a benchmark
of hardware capacity. Existing saved limits remain in force. The worker follows
capacity changes every **5 seconds**; a lowering retires surplus slots between
jobs and never cancels admitted work. Registrations for individual worker slots
share one computer limit and must never multiply it. Queued-job capacity facts
are derived from one presence snapshot and deduplicated per computer.

Idle PR monitoring holds no execution lease. Actionable feedback queues one
repair, which competes for the same finite capacity; returning to monitoring
releases it. The capacity, queue projection and monitoring-lease regressions in
`internal/factory/serve`, `lease` and `worker` defend this separation.

## Tool process identity in history

`DisplayEntry` is bounded at **328 bytes** on a 64-bit machine. The process
identity raised the bound from 296 to 304 bytes:
one optional pointer carries the job's registry number and start, or the kept
service's id, from the exact tool-result occurrence. Ordinary entries allocate
no process identity. This pays eight bytes per entry so history can name the
same process as the live tool end without parsing output or guessing from a
clock that excludes approval waits. Completion receipts retain their own task
identities independently of the assistant answer's tags, adding a 24-byte
`CompletionTasks` slice. Packing `RoutineCompletion` with the other boolean
fields removes eight bytes of merge padding; the flag itself adds no bytes.
Both exported receipt fields remain available to surface constructors. The
person-mark layout guard in `internal/session/submission_test.go` pins the bound.

## Recorded media reference lookup

`media.resolve` searches an existing owner's recorded canonical path, without
registering or discovering a file. Index/path keys are at most 4096 bytes and
owner IDs 128 bytes; expected versions are at most 256 bytes. Searches share the
existing two global media lookup slots and bounded 512-record/4 MiB cache; duplicate
lookups share the actual scan. An 8-second maximum/default deadline or caller
cancellation gives Busy instead of false absence. Backward scans retain the same
bounded chunk/partial-line memory as media.get; no permanent path cache, directory
walk or new worker is created. `media_resolve_test.go` pins late-history, ownership,
shared capacity, canceled waiter/holder separation and truthful refusal laws.

## Deliverable Keep admission

Keep retains at most 64 total holders and waiters process-wide, including callers
waiting for the same source. Its exact index path retains at most 4096 bytes; session and deliverable
identifiers retain at most 128 bytes each. Saturation answers Busy without starting another waiter or
copy; context-canceled waiters release only their reservation. A holder retains
its reservation until copying, recording and any rollback have actually settled.
The per-key gate is removed when its last reservation ends; no historical keys
are retained. `deliverable_keep_gate_test.go` pins these bounds and cancellation.

## Process actions and stop-all

`processes.actions`, `processes.stop_all` and `processes.log_target`
(`internal/appwire/processactions.go`) are bounded by these constants.
A change to any of them changes this section in the same commit.

| Constant | Value | What it bounds |
| --- | --- | --- |
| `appwire.ProcessStopAllCeiling` | 10 s | The ONE deadline of a `processes.stop_all`, from the moment it is asked. Jobs and services run at once beneath it, and both are joined before the answer. |
| `session.ProcessStopAllWait` | 5 s (2 × 2 s grace + 1 s) | The shared deadline of every job a stop-all signals. They are stopped concurrently, never 4 s each in turn. |
| `session.ProcessStopAllMax` | 256 | Running jobs one stop-all signals. More are counted in `unenumerated` and left running. |
| `session.ProcessStopAllScan` | 4096 | Registry entries and live marks one stop-all examines. A scan cut short answers `remaining_unknown`. |
| `appwire.ProcessStopAllIDs` | 256 | Each id list in a receipt. A cut list answers `remaining_unknown` and keeps its counts whole. |
| `service.LogCapBytes` | 2 MiB | A service's live log, cut back only while a codeaf runs. It is stated as `cap_bytes`. |

The services of a stop-all are the agent's own (`StopOwnerWait`). They are bounded by the supervisor's 64-service store, and they wait out each service's 5 s stop grace under the 10 s ceiling. `processes.actions` and `processes.log_target` share `processes.page`'s 4-read admission and its 2 s deadline.

## Saved harness list reads

The desktop `harness.list` shares the session-action read admission (`appActionReads`).
`subharness.SavedBounded` enforces `SavedListRows` (128 rows), `SavedListFileBytes`
(1 MiB per head page or latest trace), `SavedListReadBytes` (16 MiB read in total),
and `SavedListEntries` (32768 directory entries across all scanned folders) before
loading. Directories are scanned in batches of 128; bytes are stat-checked and read
through a limit to cover file growth. The reply remains capped at 512 KiB. An exceeded
budget refuses the entire request with `too_large`; admission refuses with `busy`.

## Factory Follow PR progress relay bounds

Follow PR reuses the worker's existing assistant stream, with no heartbeat model
calls or extra GitHub polls. Per stream, provisional prose capture is bounded at
4 KiB until a committed assistant/tool boundary. The relay uses the existing
400-character stage-summary limit and admits one distinct message per 30 seconds
per round; a round's initial trigger can be reported immediately. Stop, takeover
and round replacement invalidate callbacks. Service outcome messages retain their
existing path and are not throttled by model narration. Focused progress tests
cover bounds, retries, hidden reasoning, cancellation and callback invalidation.

## Partial factory PR reads

A PR refresh retains at most one diagnostic for each independent failing read
(checks, reviews/review policy, feedback, readiness). Each diagnostic detail is
bounded to 2000 bytes. This adds no model call, polling timer or unbounded retry:
the existing GitHub read retry and factory poll remain the only retry owners.
Policy capability failures are not cached, so subsequent polls observe plan or
permission changes. Readiness keys are issued only for complete snapshots.

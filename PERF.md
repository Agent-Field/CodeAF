# Measuring performance

## Idle desktop chat admission

`TestAppLazyQuietColdSweepBoundAndCoverage` in `cmd/codeaf/applazy_quiet_test.go`
counts work-input probes with 13, 100 and 1,000 saved chats and a fake clock.
Startup stamps every eligible closed candidate once. Each subsequent two-second
beat stamps every hot candidate plus at most `ceil(cold / 30)` cold candidates
in round-robin order. `appLazyColdSweep` is one minute; `appCrewPoll` is two
seconds. Every cold chat is visited within 30 ticks for a stable population.

| Quiet cold chats | Base work-input stats per tick | Cold stamps per tick | Local/budget probes per tick | Shared team stats per tick |
| --- | --- | --- | --- | --- |
| 13 | 104 | 1 | 7 | 1 |
| 100 | 800 | 4 | 25 | 1 |
| 1,000 | 8,000 | 34 | 205 | 1 |

Each selected chat uses six local metadata probes. The budget and profile/team
stamps are computed once per admission pass; traffic stamps once per crew per
pass. The existing crew-view metadata checks and admission reads after a stamp
changes are separate from these work-input counts. Work counts are the gate;
elapsed test time is diagnostic.

Live factory stages, kept/starting services, existing crew holds, due retries
and chats seen live within `appLazyHotWindow` (10 minutes) stay on the hot beat.
Crew traffic/team events retain their existing immediate reconciliation. Other
work-file changes wait for the next cold visit (up to 60 seconds, plus a tick at
the boundary), including when events are lost, watching is unavailable or work
directories are re-created. Watcher errors still reconcile crews immediately.
There is no periodic full cold repair. Only the base profile, teams and per-team
traffic directories are watched; no watch or native buffer is added per saved
chat. `TestAppLazyWatchSetDoesNotGrowWithChatHistory` checks the same three
watches for one team at all three history sizes.

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

Keyed vault restores read the sealed vault at most twice when no selection
policy is needed, and at most three times when incoming model choices need
provider and revision checks. These caps are independent of the number of keys;
`TestARestoreOpensTheVaultAFixedNumberOfTimes` and
`TestSelectiveRestoreM1BoundedVaultReads` exercise 16 and 256 keys. A model's
optional provider metadata is backfilled without advancing its choice clock.

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

The All teams overview retains offscreen card heights and keyboard targets,
but skips their border composition and grid padding. Latest-message ordering
walks current and former memberships without copying their records.
`TestTeamsOverviewPaintAllocationBudget` measures five complete frames at 200
teams and 40,000 memberships against an 8 MiB per-frame allocation limit.
`TestTeamsOverviewViewportMatchesFullCards` compares scrolled narrow and wide
viewports with the complete rendering; hierarchy and fold tests preserve
keyboard and pointer reachability.

## Startup and interaction audit

Build the baseline and candidate through `make build` with the same Go version,
platform and pinned engine. Keep distinct executable files, and run the same
test binary and fixture against each. Prepare platform-specific embedded engine
artifacts serially: the staging step replaces the prior platform's artifact.
An absent engine is a failed measurement, not a faster startup.

```sh
CODEAF_E2E_MODEL=deepseek/deepseek-v4.1-flash go test -tags=e2e ./internal/e2e \
  -run '^(TestStartupWhileStorageCaptureIsPending|TestTabAndEngineAttachmentLatency)$' \
  -count=3 -v -timeout=5m
```

The capture test uses real movable chats and a real storage daemon behind a
transparent proxy. It withholds capture-plan requests, checks that history and
the composer appear and accept typing, then verifies saving completes after
release. Both in-process and newly hosted launches run. Tools and movement keep
their capture barriers; recorder regressions check those independently.

The tab test measures fresh-engine startup, attachment to an existing engine,
opening another saved chat, twelve alternating tab selections and typed drafts
with 2,001 message records in each chat. It pauses only its own identified engine
for 1,200 ms while opening the resume picker, then measures picker and filter
response separately from the engine's response. No model request belongs in
these measurements; the fixture rejects and counts any that occurs.

Start clocks before input delivery. A process ID, terminal-mode change, existing
tab title or old frame cannot satisfy readiness: require destination-only
history and a composer, followed by an echoed unique draft. Observation includes
5 ms polling plus terminal command overhead. Keep every sample and timeout,
report sample counts and maxima, and separate first process launches from later
ones. Fresh profiles and engines do not imply cold operating-system caches.
Report live-provider completion and recorded model identities separately; a
scripted provider cannot establish live model acceptance.

Store reopening probes distinct nonempty session IDs through the existing
message index before checking for missing session rows. It still scans message
IDs, and still discovers older writers' missing rows on a later open; no cached
migration flag skips that check. `BenchmarkOpenSettledSessions` measures the
whole public open/close operation. `TestSettledStoreTerminalStartup` separates
fixture construction from the first interactive frame of the running terminal.

Furrow detection stays off the hello path. The belt reads the remembered answer
(`furrow.PeekTools`); the session asks furrow from a goroutine joined at close
and appends the four workspace tools through `armFamily` if the folder is
watched. Measured on Spark linux-arm64 with a cold engine per workspace and the
owner profile, first frame fell from 3.87 s to about 0.5 s with the furrow
that was embedded then; with furrow 0.3.3's fast status the unfixed tree is already
at about 0.43 s, and this change keeps the build from depending on furrow's speed. The gate is
`TestBuildingASessionDoesNotWaitForFurrow`, which holds `furrow status` open and
asserts the build came back while that process was still alive; no clock decides it.

Native peer acknowledgements reuse the validated routing view only for the same
store and durably applied index. Every applied change, including revocation,
invalidates that view. `TestR11R27UnchangedPeerAcknowledgementWork` exercises the
real transport and caps an unchanged acknowledgement at 1,000 allocations with
no application append; the measured cost fell from 3,273 allocations to 192.

An authenticated native directory read with already durable ingress time uses a
fresh quorum view and the normal state evaluator before omitting an identical
append. New time and changed records still commit before inspection.
`TestR4R26R27ReadCommitsOnlyChangedTime` counts applied entries for unchanged,
advanced and repeated time, and requires invalid original authority to fail.

## Live tool approval settings

Tool decisions refresh through the existing approval gate. The config readers
reuse parsed files until metadata or the settings generation changes; the gate
holds one compiled policy and reuses it while its three resolved rows match.
There is no watcher, polling loop or per-call policy compilation for unchanged
settings. Clarification conversations follow their parent's gate; ordinary task
workers use a fixed allow policy. A dedicated approval mutex serializes settings
builds, publication, policy pushes and conversation posture changes. Each decision's
immutable snapshot is selected under that lock before another build can start.
The general agent mutex only protects state reads and writes, so filesystem
work inside a gate build cannot block interruption, stopping or consent answers.

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

## Deliverable write recording

Conversation write and edit recording hashes the exact bytes already held by
the mutation tool and uses its descriptor metadata; it does not reread or
discover files. MIME sniffing reads at most 512 of those authored bytes. An absent
file's mutation queue resolves its parent directory before writing, so directory
aliases share the same queue. Canonical-path identity selection and append share
the ledger lock. Task/worker recording and settlement retain their base behavior.

The path lookup index is built once per retained ledger tracker, then catches up
from its last byte offset, including external appends. It holds one offset and
length per session/deliverable identity, grouped by recorded canonical path;
rewrites replace that identity's offset rather than retain snapshots or update
history. Memory grows with distinct recorded paths and identities. It shares the
existing 64-tracker LRU registry; eviction, file replacement or truncation rebuilds
it on the next lookup. Parsing uses 256 KiB chunks and at most one additional 256 KiB pending
line head process-wide under the ledger lock; idle path indexes drop that head. One catch-up attempt reads at most 64 MiB or runs for 2 seconds,
advancing at least one chunk. Expired attempts retain their progress and retry
synchronously: a slow lookup records late and never drops a successful write's
record because of a deadline. Registry admission also waits instead of losing
recording. Real filesystem errors retain the existing best-effort behavior.

The desktop's existing bounded notification window deduplicates by deliverable
and producing tool call, retaining at most 193 bytes per key (128-byte ID, a
separator and a 64-byte digest). Run and call inputs remain capped at 1024 bytes
each. A later write to the same ID can therefore refresh its card.

`TestFirstWritesReadHistoryOnce` creates 20 distinct files after 2001 unrelated
rows and caps total lookup reads at twice the final index size.
`TestExpiredWrittenLookupStillRecords` forces expired attempts under a 127-byte
budget and asserts that the write still records. Race regressions use hooks and
channels; alias coverage uses a symlinked temporary directory.

`BenchmarkDeliverableOverwrite` and `BenchmarkDeliverableFirstWrites` measure 50
mutations after seeding 0 or 2001 unrelated rows and warming the index, using Go
1.26.5 on linux/arm64. Run them with
`make test-focus PKGS=./internal/session RUN='^$' TEST_FLAGS='-bench BenchmarkDeliverable -benchtime=50x -benchmem'`.
Before the path index, first writes used 613–615 allocations and about 55/291 KiB
per operation for short/long history. With it, first writes use 126 allocations
and about 10–11 KiB regardless of that history size. Overwrites use 143 allocations
and about 12 KiB, versus 121–122 allocations and about 27/261 KiB before.
Both append approximately 650–670 index bytes per mutation. Timing is diagnostic;
the regression gates count bytes read, identities and exact authored hashes.

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

## Raft coordination binary size

Every build links the Raft coordination core: the modified Raft copy, protobuf,
the signed state machine and its SQLite snapshot host. The relay and devices
always coordinate through it, so no build leaves it out. On linux/amd64 it adds
3,379,200 bytes to the stripped binary (75,755,785 to 79,134,985 on the same dev
base), and the cap moves by the same amount, from 75,811,848 to 79,191,048, so
the 56,063 bytes of headroom the base had are kept.

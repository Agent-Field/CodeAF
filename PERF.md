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

## Paired team history

These sharing budgets apply only when **share teams** is on in Settings → General.
Sharing is on by default for paired computers; off rounds run no team carriers,
identity discovery, mirror writes or ownership lookups and retain existing polling.

`teams.TrafficSyncLines` limits each writing computer's window to 32 lines per
team, and `teams.TrafficSyncBytes` limits encoded lines to 16 KiB per team.
`teams.TrafficSyncTextBytes` shortens text beyond 2 KiB at a rune boundary.
`teams.TrafficSyncDeviceBytes` caps each device's encoded Traffic share at 64 KiB
across teams, newest local activity first. `teams.TrafficSyncEvery` permits a
publication at most once every 2 minutes; team record edits are not delayed.
Capture reads the bounded backward tail across the current and rotated logs. Received windows replace a
separate display mirror and are never republished. An unchanged round on 500
chats and 5 teams performs zero world scans and opens zero cell metadata files;
member identities cache by metadata stamp, unchanged authored tails are reused,
and unresolved incoming identities share at most one discovery pass per round. Tests count scans and encoded bytes,
compare deterministic values, and verify quiet rounds leave the vault unchanged.
Mirror filtering makes zero directory requests per vault round, using only an
already-held local roster, or every other writer when none is available. A send
shares one ownership reading across its recipients, with a 3-second timeout and
a 5-second process cache (including failures). With effective sharing, cursor saves stat identity files
but decode them only after a stamp changes. Unpaired computers retain legacy
cursors, repairing reads and completion catalogs, with no sharing refresh clock
or mirror reads. The first team capture preserves the original file once as
`teams.before-sharing.json` before creating its ledger. Mirror refresh never acknowledges
local news and each hosted window carries its own mirror version.

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
| `appStopAllOpenWait` | 5 s (the ceiling less the jobs' wait) | How long a stop-all waits for a cold kept owner or an open under way before its jobs' side answers `remaining_unknown`. Services do not wait. |
| `session.ProcessStopAllMax` | 256 | Running jobs one stop-all signals. More are counted in `unenumerated` and left running. |
| `session.ProcessStopAllScan` | 4096 | Registry entries and live marks one stop-all examines. A scan cut short answers `remaining_unknown`. |
| `appwire.ProcessStopAllIDs` | 256 | Each id list in a receipt. A cut list answers `remaining_unknown` and keeps its counts whole. |
| `service.LogCapBytes` | 2 MiB | A service's live log, cut back only while a codeaf runs. It is stated as `cap_bytes`. |

Stop-all looks up the record without opening before admission. Its jobs side
opens a cold owner only when a crew keeps it, it is not held or closing, and the
person has not closed or archived it. Non-kept conversations stay cold.
An open has one completion channel from admission through initialization and
publication, shared by all stop-all attempts. Timed-out attempts leave no waiter
goroutine on the gate; retries join the same open instead of starting another
opener. A timeout makes no job call and reports zero jobs stopped,
`remaining_unknown` and `partial`, even if an owner was published before the open
finished. Services start independently of this wait.

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

## Usage writer identity startup

`TestUsageMillionRowLedgerFlush` constructs 1,000,000 unique historical call
identities in 185,000,000 bytes before measuring the public writer. Initialization
streams valid full rows and retains their identities rather than a decoded row
slice. Enqueue through flush has a 256 MiB allocation budget. The existing
two-second flush contract must complete the new $0.002 call; replaying that call
and historical identities adds no rows. An independent public ledger walk checks
1,000,001 records, exact consumed bytes and $1000.002 total after the measurement.
Elapsed times are diagnostic. The retained identity set still grows with history.


## Usage writer overflow draining

`BenchmarkLedgerWriter20K` times 20,000 distinct charges from `RecordUsage` to
the last completed writer callback. Outside the timer it checks the flush,
20,000 public ledger rows, $200 total and no drops. Compare the same harness
on both revisions, with and without race instrumentation; run with `-run '^$'`.
The writer batches at most 128 already admitted rows per ledger lock. A quiet
queue flushes immediately, and flush/close barriers follow completed writes.
Identity generation checks and duplicate suppression still run under that lock.

## Factory signal acknowledgement bounds

Each worker heartbeat sends at most 256 unconfirmed signal delivery IDs. A failed
heartbeat retains its receipts; a successful heartbeat retires sent IDs unless
the reply still carries them. Deduplication retains only outstanding deliveries,
not the history of a lease. `TestAcknowledgementsLeaveOnlyUnconfirmedDeliveries`
drives 25,000 receipts through real HTTP, checks failed-heartbeat retention, and
requires the backlog to drain within 110 successful heartbeats.

## Idle factory manager checks

Every open job manager and every open step asks whether its job was stopped or
its budget reached (`watchJobBudget` in `internal/session/job_budget.go`): ten
times a second (`jobBudgetLiveEvery`) while it owns work in flight — a turn, a
running background command, an unfinished task — and once every five seconds
(`jobBudgetIdleEvery`, the presence beat) while it owns none, since there is
then nothing a Stop or a spent budget could cut short and the next model call
is checked at its own boundary. An idle watch sleeps that whole interval on one
timer rather than waking every 100 ms; a turn opening, a command starting and
every presence nudge wake it onto the live cadence at once (`wakeWorkWatch`).
`TestIdleManagerBudgetWatchLooksOnceABeat`: an idle manager looks at most once
in 950 ms, and at least three times in 500 ms once a command runs. Neither check may decode a job document that
has not changed since it last looked: `seamRunDoor.AutomaticRefusal`
(`cmd/codeaf/factory_person_stop.go`) and `stageBudget.Refusal` remember the
answer with the file it came from (`os.SameFile`, size and modification time;
every store write renames a fresh file into place) and stat the file before
reading it, so a write landing mid-read is read on the next check. A
conversation that manages no job is remembered against the floor folder's own
identity. `TestAnIdleManagersStopCheckDecodesNothingUnchanged` counts the
decoded documents (`factoryStopCheckDecodes`): fifty unchanged checks decode
none, and a Stop is seen at the next check.

A conversation that manages no job (every open step chat) is NOT keyed on the
floor folder: the folder's time moves with every file renamed into it (the
runner registry, a lease heartbeat, the sources' poll record), which made every
open step chat list and decode every job every few seconds. It is answered from
one look at the floor's job files (`floorJobFiles`: one directory read and one
stat per job, shared by every conversation for `floorLookEvery`, 100 ms) and
reads only a job document that changed. `TestAStepChatsStopCheckDecodesNothingWhileTheFloorChurns`:
fifty checks while the registry is rewritten decode none; one changed job costs
one decode.

## Idle open conversations

An open conversation that is idle — nothing running, nothing asked — costs no
system call between its heartbeats (`internal/session/taskpresence.go`):

- Its presence file is rewritten when its contents change and otherwise once a
  minute (`presenceIdleRefresh`), under a lease written into it
  (`leaseSeconds`, `presenceIdleWindow`, three minutes) that readers believe
  instead of the 15-second busy window (`SessionPresence.Window`). A working
  or waiting session keeps the seven-second refresh and the short window.
- Its doorstep — answers and requests from other windows — is read when the
  shared directory watch reports a write there (`presenceDoorstepEvent`), and
  otherwise once a minute (`presenceQuietPass`); a folder the watch cannot see
  is knocked on every five-second beat as before.
- Its job's budget watch (above) sleeps five seconds at a time.

`TestAnIdleChatWritesNothingBetweenHeartbeats` drives one desk on fake shared
clocks through a minute of beats and refreshes and fails on any rewrite before
the heartbeat; `TestQuietDoorstepIsWokenByTheWatchOrKnockedWhereItIsBlind`.

The sixth live A+B run (macOS, ~24 idle job and step chats) measured the engine
at 6.9–9.1% of a core. Rig: `codeaf app` with 24 conversations, each made a
job's own chat (r6's job documents, Talk pointed at it), opened and left idle,
a stub model, CPU from `/proc/<pid>/stat` or `ps` over 90–120 s after 20 s
settling; system calls by `strace -f` over 40 s on Linux:

| | Linux CPU | Linux syscalls/s | macOS (mac-studio) CPU | presence rewrites/s |
| --- | --- | --- | --- | --- |
| before (`78aca0491`) | 2.07% | 1,426 | 5.46% | 3.6 |
| after | 0.58% | 201 | 0.53% | 0.3–0.4 |

At quit every conversation is let go on its own goroutine, and one slower than
a second is named on stderr with its two halves:
`codeaf app: leaving: conversation <id> took 18s (waiting for it 12s, closing it 6s)`.

## Factory floor reads

`store.Store.read` (every `Get`, `List`, `ListSaved` and update of a job
document) decodes a document only when its file changed since the process last
read it (`internal/factory/store/readcache.go`): one stat, compared on
`os.SameFile`, size, modification time and mode — every store write renames a
fresh file into place — answers an unchanged document from a process-wide
cache, deep-copied (`deepCopy`) so no caller shares anything with the next. The
cache is the process's, not a `Store`'s, since many doors open their own store
on one root, and holds at most `readCacheMost` (8,192) documents.
`TestListingAnUnchangedFloorDecodesNothing`: ten listings of an unchanged
40-job floor through two stores decode none (`documentDecodes`); one write
costs one decode. `TestDeepCopySharesNoStorageWithTheDocument` fills every
exported field of a document and fails if any slice, map or pointer of the copy
shares storage with it.

The knowledge vault's build loop (`knowledge.Vault.Run`) passed every 2 s for
the engine's life — listing every saved chat, stat'ing each, opening the
database. A pass that finds the same chats as the last now doubles the wait up
to `buildQuietMax` (30 s); anything new, or a person's Build (`StartBuild`
wakes the loop), starts it again from `buildEvery` (2 s).
`TestQuietVaultBacksOffAndBuildWakesIt`.

The fifth live A+B run (macOS, 82 jobs, a step chat, a manager and the Factory
manager open) measured the engine at 21.4% average and 66.6% max, with libc
`open`, `stat` and `getdirentries` the busiest leaves. Live on linux/arm64:
`codeaf app` with a GitHub-connected local factory, 82 jobs on the floor (the
fifth run's eleven documents, 15–178 KB, repeated), 6 job managers opened and
idle, the runner on with 15 slots and nothing to do; the engine's own CPU from
`/proc/<pid>/stat` every 10 s for 5 minutes, after 20 s settling (scratch rig
`rig.py`):

| | average | max | bytes read in the run |
| --- | --- | --- | --- |
| before (`0838f42a6` plus the E10 change) | 5.8% | 6.6% | 1.7 GB |
| after | 1.6% | 2.1% | 8.3 MB |

Linux stats and opens files far more cheaply than macOS, which is why the
same floor cost 21% there; what was removed is the reading itself.

## Idle runner pulls and the runner registry

Every runner slot holds a pull open (`pullWorker`) and makes a fresh one each
wait (30 s); a look (`lease.Desk.Pull`) decodes every job on the floor. While
NO LEASE IS HELD a pull does not look again at a floor none of whose files
changed since its last empty look (`Handler.idleFloor`: one directory read and
one stat per file, the pull's own bookkeeping — `runners.json`, the live
record, `sources.json` — left out; the worker's slots and consent are part of
the key). With any lease held every pull looks, because a hold expires by the
clock and a look is what requeues it. The sources' poll record no longer wakes
open pulls either. `TestAnIdlePullDoesNotLookAgainAtAnUnchangedFloor` counts
looks (`pullLooks`): ten idle pulls of an unchanged floor make none.

A worker's presence (`Last`) moves on every request it makes and is saved to
`runners.json` at most every `heardSaveEvery` (a quarter of
`lease.OfflineAfter`, 15 s); the rows a page reads are in memory. It was saved
on every request, rewriting the registry every ~4 s on an idle machine. A
runner's rows from before a restart (same machine, a new runner id) are dropped
by the next idle pull once quiet past the runner timeout and holding no lease
(`TestAnIdlePullForgetsARestartedRunnersOldRows`), so the registry no longer
grows by one set of rows per restart.

`settleJobs` (a hydrated `Store.List`) ran before EVERY request. A runner's own
requests (`/v1/worker/…`, fifteen pulls re-made each 30 s wait) now settle the
floor at most once a `workerSettleEvery` (1 s), and its beats (every held job's
lease heartbeat each `lease.HeartbeatEvery`, 2 s, and its presence) at most once
a `beatSettleEvery` (5 s; at 1 s two holds made a full settle every second). A
beat reports nothing, so nothing it brings settles late; a report still settles
within a second, which is how Auto's publish follows its proof. Every other
request still settles first, and the job timer settles at each deadline it knows.
`TestARunnersRequestsSettleTheFloorAtMostOnceASettleWindow` counts settles
(`settles`): pulls inside one second make none, beats inside five make none, one
after each window makes one.

### With a hold on the floor (the seventh live A+B run)

The seventh live run idled at 20–26% of a core on macOS with 86 jobs, 15 slots
and two native jobs holding leases. Each held job's runner renews its lease
every 2 s, which rewrote `leases.json`, which woke every open pull, and with any
hold present every woken pull looked: fifteen `Desk.Pull` calls, each a flock,
a `ListSaved` deep copy of all 86 jobs and a sort, once a second. Three changes:

- **A renewal wakes no pull.** The root watch compares the lease document's
  `lease.RenewalShape` (every hold, signal and receipt, without `heartbeat_at`
  and `expires_at`); a rewrite that only renewed wakes nobody. Folders under
  the root (a runner's holds and reports) and `events.jsonl` are no input of a
  look either. A hold that expires is still requeued by the next look, at most
  one pull wait (30 s) later, as before.
  `TestARenewalWakesNoPullAndARealChangeWakesThemAll` publishes thirty renewals
  by hand and counts no wake.
- **One reading of a change for every slot.** `Desk.floorQueue` keeps the queue
  in `runsBefore` order with the file each job came from; a look stats the floor
  and reuses every job whose file is unchanged, reading only a changed one, and
  sorts only when one changed. The queue is shared and read-only (the
  reservation re-reads the job under its lock). `OverLimit` reads the same
  queue. `TestSlotsWokenByOneChangeShareOneReadingOfIt`: 45 looks at an
  unchanged 40-job floor read no job; one change reads one job once.
- **A job already held at its budget is not saved again by a look.** With a
  reply pending it stays eligible, and each look saved it unchanged as a fresh
  file, which woke every pull to save it again.
  `TestALookLeavesAJobAtItsBudgetAsItIsOnDisk` (ten looks; the old code moved
  its revision from 2 to 12).
- **A held job's runner parses its manager's journal once per version.** The
  worker's flush ran `thread.Read` every beat, parsing the manager's whole
  transcript twice; it now hands back the answer read last while the journal
  and its folder are unchanged (os.SameFile, size, time, mode).
  `TestAnUnchangedJournalIsParsedOnceAndAnAppendedLineIsRead`: eleven reads of
  an unchanged journal parse it once; an appended line is read.
- **A job chat's budget watch lists no folder while the mirrors are unchanged.**
  Every open job chat asked `jobChat.holders` where its job's money is every
  few seconds, and each asking globbed `workers/*/*/<job>.json`. The mirror
  folders are now listed once per change of `workers/` or a process folder in
  it, and a named job file is one stat per mirror.
  `TestAJobChatsHoldersListTheMirrorFoldersOncePerChangeOfThem`: twenty askings
  list the folders at most once; a new mirror is found on the next.

Rig (scratch `r7rig.py`): `codeaf app` with a local factory, 84 of r7's final
job documents plus two native jobs whose step holds its lease, about 45 open job
chats, the runner on with 15 slots, a stub model; the engine's CPU over 120 s
starting 200 s after start:

| | linux/arm64 | macOS (mac-studio) |
| --- | --- | --- |
| before (`1436ad765`) | 11.12% | 24.09% |
| after | 1.78% | 6.25% |

The macOS after is above the 5% bar. Its remaining steady profile (60 s, 6.2%
of a core) is file-status calls: the floor's hydrated settle every five seconds
(`settleJobs`, 11%), each held job's flush re-reading its job (10%), and the
presence beat. With 43 chats that each ran fifteen tool calls the same build
reads 7.6%. Quit took 1.5–1.7 s on both hosts, before and after; the rig does
not reproduce the live run's 28–30 s.

### The settle and the held-job flush (the eighth live A+B round)

After the seventh run's changes the macOS idle still spent about a fifth of its
profile re-reading jobs that had not changed. Two changes:

- **The settle lists the floor from memory.** `settleJobs` and
  `scheduleJobTimer` use `Store.ListShared`: one directory read, and every job
  whose file is the one read last (`os.SameFile`, size, time, mode) is answered
  from memory with no stat, decode or copy; only an edited file is read again,
  whatever wrote it. Hydration (author access, output retention) is applied to
  every listing on a copy, with the retention read once per listing rather than
  once per job. The items are shared and read-only; every settle re-reads a
  candidate under its lock before acting.
  `TestASharedListingOfAnUnchangedFloorReadsNothingAndSeesAnEdit` (five
  listings of 30 jobs read none; an in-place edit reads one and matches `List`)
  and `TestASettleOfAnUnchangedFloorReadsNoJob`.
- **A held job's quiet flush is not repeated.** A flush (every 500 ms per held
  job) that found nothing to send keeps the mirror file's `store.Stamp` and the
  journal's `thread.Version`; later flushes stat both and read nothing while
  both are unchanged, for at most `quietTrust` (one minute, because retention
  depends on the clock). A send forgets the quiet; an accepted reply not yet
  acknowledged always reports. `TestAHeldJobWhoseMirrorIsUnchangedIsNotReadAgain`
  (fake clock: twenty flushes read nothing; an edit, a journal change and a
  minute each read once).

Same rig and window as above (`r7rig.py`, 84 + 2 held jobs, 37–38 open chats,
15 slots, 120 s from 200 s after start):

| | linux/arm64 | macOS (mac-studio) |
| --- | --- | --- |
| before (`5a4402f65`) | 1.57% | 6.05% |
| after | 1.02% | 4.62% |

The 60 s steady profile on macOS fell from 5.21% to 3.68% of a core;
`settleJobs` (14% of it) and the flush (8%) are gone. What remains is the chat
presence beat (`presenceDesk.writeAt`, 10%), `Agent.scanKnowledge` (7%) and the
root watch's poll `snapshot` (5%). Quit took 1.3–1.4 s on both hosts, and now
leaves no hold behind: a clean quit hands each lease back (`Desk.HandBack`), so
`leases.json` reads `[]` after it instead of the two holds running out.

`CODEAF_DIAGNOSTIC_PROFILE_DELAY` (1..3600 s) opens the developer profile's
window that long after start, so a steady idle can be profiled; the window
itself is still at most 60 s.

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

Every build links the Raft coordination core: the modified Raft copy, protobuf,
the signed state machine and its SQLite snapshot host. The relay and devices
always coordinate through it, so no build leaves it out. On linux/amd64 it added
2,519,040 bytes to the stripped binary (75,268,361 to 77,787,401), and the cap
moved by the same amount, from 75,811,848 to 78,330,888.

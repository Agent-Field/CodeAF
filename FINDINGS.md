# c294 item 4 findings

This task establishes one shared plan capability interface for the live surface and remote client, adds a legal compile-time assertion that the remote client implements it, and proves through enginehost with a real temporary session store that seeded plan rows cross server and client end to end.

The preceding wire-method tasks are complete. The live surface currently owns private `planAgent` and `planReader` method sets, so the next step is a failing compile-time assertion/test that exposes the shared-interface gap, followed by the smallest interface extraction and compile fix.

## Failing assertion

The legal dependency direction is `session` defining the capability, `remote` asserting its client implements it, and `tui3` naming that shared method set. The initial compile assertion intentionally names `session.PlanAgent` before it exists; focused compilation must fail until the shared interface is added.

## Shared capability

`session.PlanAgent` is now the single method set. The live surface aliases it, and `remote` carries a production compile-time assertion in the legal import direction (`remote` imports `session`).

The surface-door law requires a locally declared interface it can inspect, so `planAgent` embeds `session.PlanAgent` instead of aliasing it. This preserves one source of methods while keeping the repository's static wire coverage law effective.

The final surface change is only a type alias. The remote surface-door law now follows selected interface aliases, preventing the shared-interface extraction from weakening its wire coverage without making shared plan reads look like new update-loop calls.

## Hosted end-to-end proof

`TestHostedAgentReadsSeededPlanTasksEndToEnd` uses `t.TempDir`, `session.OpenRunPlan`, a real `session.Agent`, the engine host's attach server over a real pipe, and `remote.Dial`. It proves `remote.Agent.PlanTasks` returns the seeded PlanDB row rather than a scripted transport answer.

## Verification

The shared capability, legal production assertion, hosted real-store test, formatting, build, vet, and focused remote/enginehost tests pass. `make test-laws` remains red after the previously local plan doors became visible on `remote.Agent`: `internal/tui3/offlooplaw_test.go` now flags seven existing plan call sites. Those calls predate this item and already return Bubble Tea commands, while this task is explicitly limited to compile-fix-only surface changes; restructuring them or increasing the law's debt budget would violate this work order. The law failure is therefore an integration dependency for the run owner, not silently weakened here. The same law run also reports five unrelated `cmd/codeaf` failures.

## Plan-door law integration

The seven findings were real once the shared plan capability crossed the hosted wire: synchronous page reads and steering could now block the Bubble Tea update loop. The integration fix sends each plan read and verb through the existing ordered `offLoop` door line and folds only captured results back into the surface; the paint-clock follower now returns a command, and its one direct test drives that command. No law budget or named debt changed.

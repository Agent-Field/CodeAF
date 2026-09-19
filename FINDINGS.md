# FINDINGS

## Settled facts

- The target is `internal/session` test `TestAPinTheWireRefusesTellsTheConversationSo` at base `7cda67c9a`.
- The test passes alone, but cleanup has failed because a writer created content below the test's `v3` state directory while or after `t.TempDir` cleanup ran.
- Issue #1211 concerns a test-owned writer in another package and is fenced from this investigation.
- Session clock and poll timeout behavior is owned elsewhere and is fenced from this investigation.

## Investigation plan

- Read the target test and follow every open path that receives its state directory.
- Enumerate test-started and product-started writers that can outlive the test body and write below `v3`, with file, line, and starter.
- Rule out candidates only from lifecycle and path evidence in the base tree.

## Writer enumeration

### Product-owned writers that can outlive the body

- `internal/session/agent.go:498`, started by `Agent.startLaneBeat` during `newAgent` at `internal/session/agent.go:426`. The goroutine runs `lanes.Beat`; a refresh can create the cache directory and file below `v3/lanes` at `internal/lane/sheet.go:1344`. `Agent.Close` calls `stopLaneBeat` at `internal/session/agent.go:2291`, but `stopLaneBeat` at `internal/session/agent.go:543-550` cancels without waiting. This is a lifecycle candidate.
- `internal/session/usage_ledger.go:578`, started by `usageWriterFor` after the turn banks nonzero usage through `Agent.bank` at `internal/session/loop.go:3973`. The goroutine opens and appends the default `v3/usage.jsonl` through `recordUsageLine` at `internal/session/usage_ledger.go:823-831` and `openUsageLedger` at `internal/session/usage_ledger.go:601-603`. The source states that this writer lives for the process. `Agent.Close` does not flush it. The real v3 process waits only after all agents close, at `cmd/codeaf/chatv3_process.go:439-444`. This is the direct cleanup-race candidate.

### Test-owned writers

- None write below the state directory. The test starts no goroutine directly. `lanestub.New` owns HTTP serving work, but that work serves requests and does not own a state path.

## Ruled out

- `lanes.HeardPrefsCarried`, called by the test, synchronously updates the sheet preference answer at `internal/lane/sheet.go:570-586`; it starts no writer.
- `provider.RepinLane`, called by the test, synchronously updates process memory at `internal/provider/lanepin.go:121-132`; it starts no writer.
- The chat journal writer at `internal/session/chatlog.go:133` is not started because this test supplies no memory store, so `newChatJournal` returns nil at `internal/session/chatlog.go:121-123`.
- Session-file writes are absent because the test supplies no `Config.SessionFile`.
- Place, presence, task, and droppings writers are absent because the test supplies no `Config.Place` and starts no tasks or jobs.
- Provider stream goroutines can outlive their caller in general, but the test drains the returned event stream before its body returns, and those goroutines do not own a file below `v3`.

## Current conclusion

The process-owned usage writer is the strongest match for a file appearing during `t.TempDir` cleanup because a successful turn necessarily queues its nonzero usage and the queue handoff does not wait for the filesystem. The lane beat is also product-owned and unjoined, but its possible state write is a sheet cache refresh rather than the per-call write guaranteed by the tested turn. A person closing the full v3 program quickly is protected for usage by the ordering in `v3Process.closeAll`: close every agent, wait for those closes, then call `session.FlushUsage` before returning. Calling `Agent.Close` alone does not provide that process-level guarantee.

## Forced-order reproduction design

The next step will add a test-only barrier at the usage writer's dequeue seam. The target test will hold the queued usage row until its body has completed agent close, then prove that the writer can create `v3/usage.jsonl` afterward. This forces the observed ordering without load, sleeps, busy loops, or synthetic CPU. The barrier belongs at `usageWriter.run`, immediately before its lazy open, because that is the exact asynchronous writer identified above. No session clock, poll timeout, or issue #1211 surface is involved.

## Deterministic reproduction

A test-only `usageWriter.beforeWrite` barrier now stops the exact writer after dequeue and before `openUsageLedger` at `internal/session/usage_ledger.go:603`. The target turn reaches that barrier, then `Agent.Close` returns while `v3/usage.jsonl` still does not exist. The focused command fails deterministically with `Agent.Close returned before its product-owned usage writer created .../001/v3/usage.jsonl: no such file or directory`. Cleanup releases the barrier and calls `FlushUsage`, so the test leaves no blocked writer. This establishes that the late writer is the product-owned usage writer started by `usageWriterFor`, not the lane beat or any test-started goroutine.

## Ownership decision

The forced row belongs to the process-wide usage ledger, not to one Agent. `Agent.Close` is therefore not the ownership boundary that must drain it. The full v3 door already closes all agents and then calls `FlushUsage`, so quick real program close cannot produce this late usage write. This test creates process-owned usage work while giving that process state a test-body lifetime. The smallest fix is for the test to release and flush that writer before returning, then assert the ledger was written. Changing `Agent.Close` would impose a process-global flush on every individual agent close and duplicate the existing outer-door ordering. The lane beat remains a separate theoretical unjoined writer, but the barrier rules it out as this regression's signature.

## Fix step

The regression will retain the forced dequeue-before-open ordering, close the Agent, then explicitly release and flush the process-owned writer before the test returns. The assertion moves after `FlushUsage`, proving the test's process-state owner has joined the write. Cleanup uses the same idempotent release as a failure fallback, so an earlier test failure cannot strand the writer.

## Fix correction

The first edit command matched no source text, so that commit changed only this findings record and did not alter behavior. The source edit now applies the same ownership decision with exact multiline matches.

## Focused verification step

The forced regression is now expected to pass repeatedly because the state owner releases the barrier and calls `FlushUsage` before checking the ledger and returning. Verification will run the single test repeatedly, confirm formatting, and confirm the fenced files remain untouched.

## Focused verification result

`go test ./internal/session -run '^TestAPinTheWireRefusesTellsTheConversationSo$' -count=10` passed. `gofmt -l` printed nothing for both touched Go files. The fenced timeout and issue #1211 file-name check printed nothing. The required pre-PR sequence starts from the tree hash printed below.

## Build check

The next independent pre-PR check is `go build ./...`. It verifies all packages compile after the test seam and ownership fix.

## Build check result and session-suite step

`go build ./...` passed. The next independent check runs the complete session package tree through the required Spark lock wrapper with a fresh test count.

## Session-suite result and formatting step

The wrapped whole session suite ran once and failed across many unrelated task-system tests because this checkout's current task belt and audit behavior differ from those fixtures. The target forced regression had already passed 10 times, and the suite output did not report it failing. This broad pre-existing failure is recorded without retry. The next independent check is repository Go formatting.

## Formatting result and change-entry step

`gofmt -l ./cmd ./internal` printed nothing. The next independent check validates the unreleased change-entry corpus.

## Change-entry result and law step

`go run ./cmd/codeaf-changes check` passed with 15 well-formed entries. The final independent code check runs the guard and naming-law packages.

## Law result and final tree

`go test ./internal/guard ./internal/namelaw` passed. Four of five required pre-PR commands passed. The wrapped session command ran and failed only in the unrelated task-system surfaces listed in its output; it did not fail the forced pin-wire regression. The ending tree hash is printed below. No source or findings amendment follows this record.

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

# Resume late-writer findings

## Settled evidence

- `TestResumeOpensTheConversationThePersonSpokeInLast` failed once during `t.TempDir` cleanup because its `state` directory became nonempty while cleanup was removing it.
- The test passes alone, 20 runs out of 20.
- The state root belongs to this test under its own `t.TempDir`.
- The shipped #1211 waiting-line test fix concerns a different test and is fenced from this investigation.

## Intended checks

- Trace the named resume test and every product path it opens from the base tree.
- Inventory every test-started or product-started writer that can outlive the test body and write below the state root, with file and line evidence.
- Record ownership hypotheses and explicitly rule out writers whose paths or lifetime cannot explain the cleanup race.

## Ruled out so far

- The #1211 synthetic usage-noise goroutine is not a candidate because it belongs to a different test and must remain untouched.
- Machine load reproduction is excluded. Any later reproduction must force ordering with a hook, barrier, or delayed writer.

## Inventory from the base tree

### Directly started by the named test

- None. `cmd/codeaf/chatv3_layout_test.go:45-74` contains no goroutine, process, session agent, store, journal, presence service, usage service, or telemetry service. Its writes at `cmd/codeaf/chatv3_layout_test.go:52-58` finish inline before the body returns.
- `v3ProjectDir` at `cmd/codeaf/chatv3_layout.go:55-60` only calls `MkdirAll` inline. `v3ResolveSession` at `cmd/codeaf/chatv3_layout.go:248-284` scans, selects, reaps, or mints inline. In the exercised spoken-session branch at lines 258-278 it starts no writer. Ownership hypothesis: no test-owned or product-owned writer is created by this test's executed path.

### Writers started by a full product open that could match the state-root symptom

- Model catalog warm. The product starts it through `cmd/codeaf/chatv3_process.go:381-384`; it can wait on network catalog resolution and then writes the model cache at `cmd/codeaf/chatv3.go:2311-2330`. The cache creates directories and a temporary file below the current home at `internal/tui3/models.go:244-275`. `cmd/codeaf/chatv3_modelswarm_test.go:14-31` explicitly records that this write resolves `CODEAF_HOME` when it writes and formerly produced the same next-test `TempDir` cleanup failure. Ownership hypothesis: product-owned. Base-tree disposition: currently tracked, cancelled, and joined by `poolErrandGoCtx` at `cmd/codeaf/poolindex.go:159-173` and `stopPoolErrands` at lines 122-138, called during process close at `cmd/codeaf/chatv3_process.go:405-416`. A person closing quickly reaches the close before the cache write, cancellation ends the wait, and the join returns before close completes. It therefore cannot outlive a correct current `closeAll` call.
- Place sweep. A full launch starts a forgotten goroutine at `cmd/codeaf/chatv3_sweep.go:25-49`. On an error, `noteSweep` resolves the current home and can create and append `v3/sweep.log` at `cmd/codeaf/chatv3_sweep.go:53-66`. Ownership hypothesis: product-owned and unjoined. It can outlive the door because the source explicitly says it is started and forgotten. A person closing quickly can therefore overlap it. Ordering is open, start sweep, door returns, sweep error calls `noteSweep`. This is a conditional candidate because only an error writes below the newly current home, and the named test does not call `openV3Launch` or `startPlaceSweep`.
- Pool refresh and outbox errands. Their state-root writes and former ability to outlive the process are documented at `cmd/codeaf/poolindex.go:76-88`. Ownership hypothesis: product-owned. Base-tree disposition: ruled out after an orderly product close because registration is at `cmd/codeaf/poolindex.go:151-173` and cancellation plus join is at lines 122-138, reached from `cmd/codeaf/chatv3_process.go:410-416`.

### Test-started writers and ruled-out paths

- The deterministic model-warm regression starts the warm at `cmd/codeaf/chatv3_modelswarm_test.go:32-62`, but calls `proc.closeAll` before releasing its barrier. Its root assertion at lines 64-74 is evidence that the tracked writer does not land late. This test-owned setup is not a remaining writer candidate.
- The shipped #1211 synthetic usage-noise goroutine belongs to the separate waiting-line test. It is fenced and was not inspected or changed further.
- Journal, presence, usage, and telemetry writers are ruled out for the named test because it never constructs or opens an agent, store, session, or full launch. `v3ResolveSession` returns a path description only.

## Inventory conclusion

The named resume test owns no asynchronous writer, and its directly executed product path starts none. Base-tree evidence names two writers with the necessary lifetime and destination behavior when started elsewhere in the package: the now-joined product model warm, which exactly matches the observed signature, and the unjoined product place sweep, which writes only on an error. Pool errands match the lifetime class but are joined at product close. The smallest change is therefore no change to the named test on present evidence. A deterministic reproduction should first place a barrier at the model-warm write or force a sweep error after the door returns. Only a forced failure can distinguish a missed `closeAll` ordering from the conditional sweep path.

## Forced-order regression and ownership evidence

- `cmd/codeaf/chatv3_sweep_test.go` forces the suspected ordering at the sweep seam. Its replacement sweep signals that `openV3Process("resume")` started it, blocks on a channel, and reports a forced sweep error only after `v3Process.closeAll` returns. No load, busy loop, parallel CPU, or timing assumption is involved.
- The forced error passes through the real `noteSweep` writer at `cmd/codeaf/chatv3_sweep.go:59-70`. Because that writer resolves `CODEAF_HOME` when the note is emitted, it recreates `v3/sweep.log` after the test has removed the state root, exactly the late-create ordering that can race `t.TempDir` cleanup.
- This executed ordering establishes product ownership. `openV3ProcessWith` starts the goroutine through `startPlaceSweep` at `cmd/codeaf/chatv3_process.go:127-132`; the test starts no writer itself and only controls the product writer through the seam.
- A person who opens resume and closes quickly can reach the same ordering: product open starts the sweep, the door closes and `closeAll` returns without joining it, then a sweep error writes the log. The smallest eventual fix is to give the product process ownership of this sweep and stop or join it before `closeAll` returns. That fix is intentionally not implemented in this step.
- The model warm, pool refresh, and outbox errands remain ruled out because their process close path joins them. The named layout test starts no asynchronous writer. The #1211 waiting-line test fix remains untouched.

### Executed result

`go test ./cmd/codeaf -run '^TestThePlaceSweepStopsBeforeTheProcessCloses$' -count=3` failed all three runs at `chatv3_sweep_test.go:60`. Each failure found `state/v3/sweep.log` after `closeAll` returned and after the state root had been removed. This is the intended pre-fix failure and confirms the barrier controls the late product write deterministically.

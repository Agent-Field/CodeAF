# Resume late-writer findings

## Settled facts

- `TestResumeOpensTheConversationThePersonSpokeInLast` once failed during `t.TempDir` cleanup because its state directory became non-empty during or after cleanup.
- The test passes alone, 20 of 20 runs.
- The state root is the test-owned `.../001/state` directory.
- The waiting-line test's #1211 fix is fenced and will not be changed.

## Checks planned

- Read only the named resume test and the product code reached when it opens the conversation.
- Inventory every test-started and product-started writer that can write below this state root and outlive the test body.
- Record each candidate with file and line, starter, ownership, lifetime evidence, and ruled-out status where evidence permits.

## Initially ruled out

- Machine-load reproduction is ruled out. Any later reproduction must force ordering with a hook, barrier, or delayed writer.
- The #1211 waiting-line synthetic usage-noise goroutine is outside this task and will not be touched.

## Writer inventory

The named test starts no goroutine. The product path it calls starts no goroutine. `git grep -n 'go ' -- cmd/codeaf/chatv3_layout.go cmd/codeaf/chatv3_layout_test.go` has no executable `go` statement. Therefore this call graph has no test-owned or product-owned writer capable of outliving the test body.

All operations beneath this test's state root are synchronous:

- `cmd/codeaf/chatv3_layout_test.go:46` starts ownership of the state root by setting `CODEAF_HOME` to the first `t.TempDir` plus `state`. The test body is the owner and starter.
- `cmd/codeaf/chatv3_layout.go:56-57` computes the project bucket below `CODEAF_HOME` and calls `os.MkdirAll`. It is product code started directly by the test's `v3ProjectDir` call at `cmd/codeaf/chatv3_layout_test.go:48`. The call returns only after the directory operation returns.
- `cmd/codeaf/chatv3_layout_test.go:20-22` calls `os.MkdirAll` for each synthetic session. This is test-owned and synchronous.
- `cmd/codeaf/chatv3_layout_test.go:28-30` calls `os.WriteFile` for each synthetic `transcript.jsonl`. This is test-owned and synchronous.
- `cmd/codeaf/chatv3_layout_test.go:31-35` calls `session.SaveMeta` for each synthetic `meta.json`. This is test-owned. `internal/session/place.go:326-355` performs `MkdirAll`, `CreateTemp`, `Write`, `Close`, and `Rename` inline before returning, so no writer survives the call.
- `cmd/codeaf/chatv3_layout_test.go:56-58` calls `os.Chtimes` on the stale session directory. This is test-owned, synchronous metadata mutation.
- `cmd/codeaf/chatv3_layout_test.go:61` starts the product resume resolver. On this test's spoken-session branch, `cmd/codeaf/chatv3_layout.go:252-278` creates no goroutine and writes no file. `v3ScanBucket` at `cmd/codeaf/chatv3_layout.go:610-640` only reads. `v3PlaceFor` at `cmd/codeaf/chatv3_layout.go:449-457` only constructs a value. `v3ReapEmpty` at `cmd/codeaf/chatv3_layout.go:695-704` can synchronously remove empty sibling directories, but both fixtures contain a user message, so its empty list is empty.
- `cmd/codeaf/chatv3_layout.go:280` could call `v3MintSession` only when no folder is found. That branch is ruled out here because the resolver returns the existing `live` folder and the assertions pass. Its synchronous `SaveMeta` at `cmd/codeaf/chatv3_layout.go:460-478` is not started by this test execution.

## Inventory conclusion

There is no candidate late writer in the named test or the product code it opens. In particular, this test does not open a store or session engine and does not start a journal, presence, usage, or telemetry writer. A product `Close` ordering and quick-close user reachability do not arise on this path. The observed one-time cleanup failure must require a writer outside the inspected call graph, such as cross-test process state, but identifying such a writer is outside this inventory's named-file boundary. The waiting-line test and its #1211 join remain untouched.

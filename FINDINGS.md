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

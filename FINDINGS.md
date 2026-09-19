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

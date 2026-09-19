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

## Ruled out

- No writer candidate is ruled out yet.

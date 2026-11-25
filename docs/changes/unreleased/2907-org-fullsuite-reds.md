---
kind: fixed
title: the org/main full-suite reds, including a Memory opt-in race that admitted earlier turns
pr: 2907
surface: [chat, factory, build]
invalidates:
  - "The org server's and sessionhost's CODEAF_* variables were absent from config.OperatorEnvPins, so the full suite's environment registry test failed on org/main; they are now listed there as read-only plumbing."
  - "Turning Memory on wrote the profile switch before the vault's own record of when it began, so a capture look between the two writes read Memory as on since the beginning of time and admitted every turn the chat had already finished; the record is written first now, and that look finds Memory still off."
---
The same change makes two cmd/codeaf test fixtures deterministic: the removed-computer quit now takes the removal
sentence a window would have shown before it checks that the quit itself prints nothing, and the real-engine read test
takes its home from `EngineDir`.
Two more tests no longer return while a restarted file arrival is still writing: the live-transfer cut-after-open test
and the app take test wait for the arrival to be complete before they end, which stops the temporary-directory
cleanup from failing with "directory not empty" on the full suite.

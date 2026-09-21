---
kind: fixed
title: closing chat stops the place sweep before it can change the home again
pr: 1276
surface: [chat, engine]
invalidates:
  - "The once-per-binary place sweep was started as an unowned background goroutine. Closing the chat process neither cancelled nor joined its folder walk, so the walk could outlive close and rename or remove stale session folders under a later value of `CODEAF_HOME`."
  - "The sweep is now owned by each chat process. Close cancels its context and joins it promptly, while the walk checks cancellation between entries and immediately before each rename or remove. No destructive operation under the home can begin after close returns."
  - "Starting another chat process in the same binary starts another sweep. Closing an earlier process no longer consumes a process-global once or prevents a later launch from cleaning stale places."
---

The earlier fix sealed only the sweep note writer and deliberately left the
folder walk running after close. That preserved fast shutdown, but the walk
resolves the places root from the current home when it runs. A later process or
test could therefore replace or remove its home while the old walk was still
renaming and removing entries there, leaving cleanup to fail with a directory
not empty.

The folder walk now has per-process cancellation and ownership. Close cancels
and joins that work without waiting for the remaining folders to be walked, and
a later process in the same binary still performs its own sweep.

---
kind: fixed
title: Factory verification separates check exits from output-reporting failures
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "A failed grep pipeline previously poisoned factory verification until the identical command succeeded. Observed check exits now resolve corrected reruns independently of output formatting, while real and unobserved compound failures remain blocking."
  - "A rejected completed report previously appeared as no result and could show a successful round. Job feedback now names the verification block, retry state and explicitly rejected round."
---

The receipt records a check's directory, arguments and target/runtime settings.
Passing an unrelated subset cannot clear a failed check. Zero matches in a
failure-counting grep is a reporting result, not a test failure. Existing shell
pipefail remains enabled, unknown receipts fail closed, and owned receipt files
are removed with the conversation. Publication approval and repair limits do not
change.

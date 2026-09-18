---
kind: fixed
title: the do door seats its worker from the settings the run will use
pr: 0
surface: [engine]
invalidates:
  - "`doErrand` resolved the process's one catalog from a separate keyless settings read (`config.LoadKeyless`) while the run loaded its own keyed settings later, so a `codeaf do` launch read its settings twice and seated its worker from the first read. Its seats are now resolved from the same settings the run uses, the way `codeaf exec` and the chat surface seat theirs, with the keyless read kept as the second rung for a profile that has no key anywhere."
---

A `codeaf do` launch used to load its settings twice: one keyless read to seat
the two models before anything was opened, and the run's own settings
afterwards. The process keeps a single catalog, built by whichever caller
arrives first and never rebuilt, so the seats were computed against the catalog
the keyless read had seated — while the run itself called with the settings it
loaded later. The do door now resolves its seats from the settings the run will
use, as `codeaf exec` and the chat surface do, so a fresh profile whose key
arrives with the shell seats its worker and its plan from the pool. A profile
with no key anywhere still seats keyless on the second rung, prints its seat
line before the missing-key sentence, and reads a warm cache with or without a
key.

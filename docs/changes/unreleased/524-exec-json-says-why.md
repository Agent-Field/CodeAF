---
kind: fixed
title: exec --json says why a run failed, not only that it did
pr: 524
surface: [engine]
invalidates:
  - "A script could not learn why an `aforge exec` run failed. The `--json` failure envelope carried `\"stop\":\"error\"` and no error field at all, so a rejected model id, a missing key, an unreachable endpoint and a wall all produced the identical object, and the only copy of the sentence went to stderr. It carries `error` now — the same words the error stream prints — and the object written by `-o file` is the same object."
  - "A run that answered was believed to carry an empty `error` key. It carries no `error` key at all: the field is `omitempty`, so its presence is itself the signal that something went wrong."
  - "`exec`'s envelope was built from the outcome alone, in a spot separate from the stderr line, so the two could disagree about one failure. It is built in one place now — `buildExecEnvelope(outcome, runErr)` — and stdout, `-o` and stderr all read that."
  - "The failure sentence began `node task-1: `. exec runs a single leaf, so the node id is machinery there and stood in front of the one fact somebody was looking for; it is stripped from both the envelope and the stderr line."
---

`aforge do --json` had shipped the right shape from the start — an `error` field beside a
non-zero exit, set in `failedErrand` — and `exec`, the door harnesses actually wrap, never
got it. The exit code is unchanged at 5.

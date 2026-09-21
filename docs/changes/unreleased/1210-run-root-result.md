---
kind: fixed
title: a run's result is read from the store when the root's worker returns after the run ended
pr: 1210
surface: [engine]
invalidates:
  - "`run.Start`'s summary took the run's result only from the root worker's return. A pass on the supervisor's clock that saw the root done in the store ended the run before that return was absorbed, and the summary's result was empty while the store held it. The summary now falls back to the done root's stored result."
---

The root's worker finishes its own task with `plandb done`, so the store reads
done before the worker has come home. The supervisor looks at the store every
300 milliseconds; when that look lands in the gap, the run ends with the right
outcome word and an empty result. On a loaded box
`TestStartAnswersTheRootsDoneResult` lost the result about once in fifteen
runs, which failed two full gates. The store is the record of the result and
the worker's return is its echo, so `Start` reads the record when the echo is
missing. `TestStartAnswersTheRootsDoneResultWhenItsWorkerReturnsLate` holds the
return back for longer than a pass, so the gap is every run of the test.

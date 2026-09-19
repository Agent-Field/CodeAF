---
kind: fixed
title: a self-finished codeaf do run seats its review before it ends
pr: 1233
surface: [engine]
---
A codeaf do run whose root finished its own work could end without ever running
its review. A root worker can record its result in the store a moment before its
goroutine returns, and a run that read that stored result first ended right there,
so the review round was never seated and the run exited with no check. The run now
waits for the root worker to return before it ends, so the review is seated and
runs. It also waits while any worker whose task is already finished is still
returning, so every finished piece of work has its review seated before the run
ends. An unfinished remnant is still drained, and a killed or hung worker ends
the run by the same wall that already bounds every worker.

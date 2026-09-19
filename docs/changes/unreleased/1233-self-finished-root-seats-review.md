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
runs. It waits only while that root worker is still in flight, so a completed tree
that left another worker behind still finishes, and a killed or hung worker ends
the run by the same wall that already bounds every worker.

---
kind: fixed
title: a harness saved note no longer races the done channel that tests wait on
pr: 555
surface: [engine]
invalidates: []
---

`TaskGraph.complete` closed `node.done` before `reportTaskNode` enqueued the
saved-harness steering note, so anything unblocked by `done` — including
`TestAnApprovedDesignIsSavedAndDetectableAtOnce` under full-package load — could
read an empty queue. The announce now lands first.

---
kind: changed
title: an old tool result in a request keeps both ends and says where the whole of it is, and a pass that only stubbed still folds to its target
pr: 623
surface: [engine, docs]
invalidates:
  - "A consumed tool result from an earlier turn was sent as its last 400 bytes and nothing else: the head that said what ran — and, for a failure, the error itself — was cut, and the reduction named nowhere to read the rest. It is now a head, the same tail, the exact count of the bytes cut from between them, and the store ref or journal the whole result can be read back from. A session that can name neither says so rather than naming a path that is not there."
  - "A compaction pass folded only when the estimate was still over the TRIGGER after the stub pass. Stubbing alone routinely landed just under the trigger and far above the target, so the pass bought no headroom and the next step fired another one. The fold is now asked for against compactTarget, which is the line it already stopped at."
---

The frozen view is rebuilt on every request of every tool round, so its budget walk
carries a running total instead of re-adding every old result per result.

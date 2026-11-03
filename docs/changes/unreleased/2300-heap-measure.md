---
kind: internal
title: Isolate resumed-session heap regression measurements
pr: 2300
surface: [engine]
invalidates:
  - Shared-process heap deltas were treated as retained resume memory.
---

Resume heap checks now compare post-GC live heaps in fresh subprocesses for
6 and 60 compaction windows. Unrelated heap shrink can no longer create a
zero baseline and a false retention failure. The original limit remains one
quarter of the discarded message content. A deliberately retained-window
fixture verifies that the measurement still rejects memory that scales with
history. Session runtime behavior is unchanged.

---
pr: 1865
kind: changed
title: A factory job on the wire can carry the size it will show when no label sets one
surface: [engine, app]
invalidates:
  - "Was true: job.size was the only size on a job, so a client could show only a size label. Is true now: job.read_size {value, from} is present when there is no label and the engine has a completed read's size (from `read`) or a pull request's measured lines (from `diff`)."
  - "Still true: job.size is the size label alone and is the only size a start reads; read_size is display only, never present beside a label, and absent for a chat guess, an unrecorded source or an outdated read. A client that ignores it is unchanged."
---

`job.read_size` is additive. The desktop will draw it dim with its source once
it reads the field; until then nothing on screen changes.

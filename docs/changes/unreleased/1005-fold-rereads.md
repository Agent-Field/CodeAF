---
kind: changed
title: The turn fold keeps the newest slice of a repeatedly read file and points the older ones at it
pr: 1005
surface: [engine, docs]
invalidates:
  - "Every folded tool result used to be replaced by a pointer to a copy filed under the session's stubs folder. When one turn reads the same file several times, the older slices now fold to a pointer naming the file with the offset and limit that read it — no copy is filed — and the newest slice of the file stays whole."
---

Measured on a planning conversation that read one file in ten overlapping
slices and carried all ten, verbatim, through twenty requests: the fold bound
generic tool-result accumulation but had no notion that slices of one file are
re-readable where they came from. The trigger, target, batch and horizon laws
of the pass are unchanged; bash and every other tool's results keep their
existing rules.

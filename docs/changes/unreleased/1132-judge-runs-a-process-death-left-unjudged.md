---
kind: fixed
title: A run a process death left unjudged is judged on the next chat start
pr: 1132
surface: [engine]
invalidates:
  - "The Model Pool judge ran only from the live landing hook, on a fire-and-forget goroutine, so a process that died between a node reaching its terminal state and that goroutine finishing left the node final in `tasks.json` but never scored into `own.json`/`outbox`. A bounded start-time sweep now judges the resumed session's own final-state nodes that carry no judged marker, each exactly once."
---

The judge is news a live process delivers, and a process that is gone delivers
nothing: a node that landed and then lost its process was final on disk and
scored nowhere. The chat door now runs a sweep at start, on a goroutine nobody
waits on, that reads the resumed session's checkpoint and the pool's own pending
file and judges every landed run that carries no `pool/judged/<id>-<attempt>`
marker — the headless doors' recorded landings and the chat door's own
process-death residue alike. The live hook writes that marker but never reads it,
so a person re-auditing a landing still re-judges it; only the sweep reads it, so
each run is judged at most once. The sweep is a no-op with the pool off or no
judge-capable key present, in which case the pending rows simply wait; it is
bounded so it never holds the prompt; and it claims the pending file by an atomic
rename so a door appending to it concurrently never has a row torn out from under
it. The checker seat and the attempt are now carried on the checkpoint so a run
rebuilt after the process is gone keeps the high seat it ran with.

---
kind: fixed
title: A runner's saved slots now change how many jobs it runs at once, higher or lower, without a restart
pr: 0
surface: [engine]
invalidates:
  - "`PUT /v1/runners/{runner}` moved only the factory's cap. Lowering took effect on the next pull, but raising did nothing: the worker started its slot goroutines once at launch and never read the saved number again, so a runner pinned at the default of 2 stayed at 2. The worker now reads its own saved capacity every 5 seconds with its worker credential (`GET /v1/runner/self`) and starts or retires slots to match, up to `api.MaxRunnerSlots` (8)."
  - "A PUT that sent only `slots` erased the runner's checkout list. Checkouts that are absent from the body are now left as saved; an empty list still clears them."
  - "`CODEAF_FACTORY_BENCHES` was the runner's capacity for as long as the process lived. It is now only the first ask: once the factory has a saved number, that number wins, including after a restart, and the variable is clamped to 8."
  - "The local runner's benches were fixed at the same 2 as the lease cap, so even a raised cap could not run a third job. They are now the ceiling, and the lease cap governs."
---

Slots are bounded to 1..8 (`api.MaxRunnerSlots`); the server answers 422 outside
that range. Eight is a deliberate safety ceiling on credentials, git worktrees
and stage conversations in one process, rather than a measured throughput limit.
Legacy saved values above eight are granted eight. Missing legacy settings use
the same default of two shown in the roster, and the execution copy follows that
single saved setting. These migrations leave existing leases running.

Lowering never cancels a job: a slot above the new number finishes what it holds
and then leaves; new work waits until the factory's cap has room. Raising starts
the missing slots within one read. An already-authorized pull also rechecks the
saved capacity before reserving another job.

`GET /v1/runner/self` takes the runner's own worker credential only and answers
`{"slots": n}`, nothing about any other runner or owner. A person's token gets 401.
The attach answer now carries the saved `slots`. An older factory without the
route answers 404; the worker then keeps the number it has. An older worker keeps
its fixed slots and the factory's cap still applies to it.

A worker that follows capacity attests it once, at attach, in the
`X-Codeaf-Live-Capacity: 1` request header, and the roster lists that runner with
`live_capacity: true`. The attestation is deliberately not in the attach JSON: a
factory from before it decodes that body strictly and would refuse an unknown
field with 422, but it ignores headers, so a new worker attaches to it on the
first and only POST (never retried or downgraded) and simply is not listed as
live. A runner listed without `live_capacity` is an older binary or a worker
that cannot read its capacity; a restart alone does not change that, since an
older binary stays at its fixed 2. It needs a compatible update or
configuration, and no screen may promise a raise after a restart.

Not in this change: pause, remove, joining another machine, and any app wire verb
or control for slots.

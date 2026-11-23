---
kind: added
title: The engine can set how many jobs one of your computers runs at once, where it can follow
pr: 0
surface: [engine]
invalidates:
  - "The app wire could read a factory's computers (`factory.runners`) but not change their capacity. `factory.runner_slots {factory, runner, slots}` now does, for the OWNER of that computer, one computer at a time. It cannot pause, remove or add a computer, change its checkouts or write to GitHub."
  - "`GET /v1/runners` carried no statement of what a runner's `slots` may be, so a client had to copy the number 8. It now carries `slots_policy {min, max, requires_live}` from the server's own constant, and ONLY a factory that enforces the guard below sends it. A directory without it is an older factory, for which the engine refuses the change before any request."
  - "`PUT /v1/runners/{runner}` wrote capacity for a worker that could not follow it (an older binary stays at its fixed 2 however often it restarts). It accepts `require_live_capacity: true`, judged under the same lock as the write against the NEWEST attach of that machine; a worker that restarted onto an older binary since the caller read the directory is refused with 409 and nothing changes. A client without the field keeps the earlier behaviour exactly."
  - "`PUT /v1/runners/{runner}` answered every refusal with no statement of whether anything had been saved. It now carries `X-Codeaf-Phase: pre-mutation` on a refusal made before the first change and clears it just before the change, so a failure of the save or of the event after it reads as uncertain."
  - "The roster's `live_capacity` for a machine came from whichever of its credential rows was seen last, which presence pings reorder. It now comes from the newest ATTACH (a new `Attached` time on the row, never moved by presence); attaches that cannot be ordered must all attest, so an ambiguity can only withhold a write."
---

A distinct optional feature, `factory.runner_manage`, adds one method,
`factory.runner_slots {factory, runner, slots}`, answered with the same three
fields once the factory said 200 or 204. `FactoryCapabilities.runner_manage` says
the engine serves it. Whether THIS factory and THIS computer can take it is read
from the directory (`slots_policy`, `live_capacity`), never assumed from the
feature.

Params are exactly those three: `runner` is the computer's id as listed (bounded,
opaque, one path segment), `slots` is an integer from 1 to `api.MaxRunnerSlots`
with no default, and a route, verb, body, token or checkout list is `bad_params`.
Each call asks `GET /v1/me` and `GET /v1/runners` with the credential held now;
the computer's owner must be the handle `GET /v1/me` names. Being the operator
gives no power over another person's computer. A computer that is not live, or a
factory whose policy is absent or is not the expected one, is `conflict` before
any write leaves.

The engine sends one `PUT` and never retries. Only a 4xx or 501 carrying the phase
marker is a definite refusal, in fixed words. A dropped connection, a timeout, a
redirect, a 5xx, an unmarked refusal (the factory saves the number and then
records the event) or an unexpected success such as 202 is `uncertain`. If the
sign-in changes during any wait, nothing about the computer is revealed; after the
write the answer is a neutral `uncertain`. The receipt repeats what was asked; read
`factory.runners` for what is granted.

---
kind: added
title: codeaf app can start a task, run a program and remember a line through typed, idempotent actions
pr: 0
surface: [engine]
invalidates:
  - "ENGINE-CONTRACT's gap list said `/task <brief>`, `/review`, `/sec`, `/senior-dev`, `/remember` and `/always` have no wire verb. For those three producers there is now `session.act` (`task.start`, `program.start`, `memory.remember`); the rest of the gap list is unchanged."
  - "A typed start that errors is not proof nothing started: `session.act` answers `uncertain` with `producer_error`, never `refused` — there is no `refused` state."
  - "Sending `/review` or any slash line through `agents.send` still does not run it. `session.act` names the action; it never interprets a command line and never sends text to a model in its place."
---

`session.actions`, `session.act` and `session.action.get` (hello feature
`session.actions`). They call the terminal's own producers — `StartTask`,
`StartTaskEffort`, `StartDelegate`, `Remember`/`RememberAlways` — through the open
conversation's engine handle, so over `--host` the far machine's build and program
catalogue answer. No runner, route, default, cap or policy is added, and no field asks
the engine to skip one; unknown fields are refused.

Each call carries a `client_id` that is claimed on disk (a digest, never the person's
words) before the producer runs. A repeat is answered from the record and runs nothing;
other words under the same id are `conflict`; an attempt whose outcome is not known is
`uncertain` and is never run again. `done` means admitted, not finished.

A producer's error is `uncertain` with the producer's words, never a refusal: no
producer proves by type that its error came before an effect. Only what the engine
can refuse before it claims or calls anything (malformed asks, an unknown program, a
door the conversation lacks, memory off) is refused, as an RPC error with no record.

Only what the engine can be shown to do is offered. Every conversation the app holds is
reached through the engine wire, whose handle has every method whatever the far engine
can do, so `/task --best` and `--cheap` are not offered there (no far fact states them)
and a `task.effort` is refused before anything is claimed; an empty program catalogue
from that wire is unknown, not "none". `session.actions` shares the bounded read gate
with `session.action.get`.

Not built here: every other action in the slash catalogue. This does not make the
desktop's 51 commands work; it makes three of them possible.

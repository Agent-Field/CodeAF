---
kind: fixed
title: the daily spend ceiling binds task and errand turns, not interactive chat alone
pr: 1842
surface: [engine]
invalidates:
  - "The daily spend ceiling (the machine-wide limit over a day) was enforced only on some paths: `Agent.railBlockLocked` skipped the check for an `InTask` task-node runner (including one under `--one-model`) and the home `Errand`, and the crew guard that also enforces the day cap is absent under `--one-model` and for an `InTask` agent with no crew. So a chat `/task` past the day's limit, and a task under `--one-model`, kept spending with no refusal while headless `codeaf do` and standing firings honoured it. The ceiling is now checked on every turn-starting path through `railBlockLocked`; it is a ceiling read of the shared ledger, never a tally, so a path already held to the crew's own day cap is refused in two places and never charged twice. The rail still stops the next turn rather than cutting one in half."
---

The gap left open by #1627, which enabled the chat and crew daily check but
kept the `InTask`/`Errand` carve-out. Removing it is what closes the two roads
that had no guard behind them at all - a task under `--one-model` and an
`InTask` turn.

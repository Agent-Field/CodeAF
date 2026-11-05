---
kind: fixed
title: codeaf keeps unreadable profile approval limits in factory stages and task children
pr: 2325
surface: [engine, factory]
invalidates:
  - "A factory stage's allow posture bypassed an unreadable profile's saved approval rules. It now requires a person when those rules cannot be read."
  - "Task workers, repair rounds and auditors dropped their parent's profile read failure. They now consult that signal at each decision and refuse calls that need a person when nobody can answer."
  - "A project parse error could hide a known profile read failure. The stage now returns the failure already in hand before reading project rules."
  - "Standalone run workers discarded profile read failures. They now check their run's profile before each call, including after work starts."
  - "A refused job could lose the config-file reason during report recovery or first-turn budget admission. Its result now keeps that reason without futile report retries."
---

A readable profile keeps its existing behavior, including a factory stage's own
allow posture. An unreadable profile cannot silently discard a saved denial:
the existing approval gate reports the failed file read and requires a person.

The named readable default-stage bypass includes readable malformed JSON.
Other postures retain their existing parse-error behavior. Filesystem read
failures remain distinct from JSON parse failures. Task, repair, checker and
fleet children keep their own normal allowance while following the parent's
current failed-read signal; recovery clears that inherited signal too.

First-turn daily-limit refusals retain the config filename and read error.
Admission decisions, spending counts and charges are unchanged.

---
name: Defect
about: Something behaves wrongly. Written so a stranger can reproduce it and a gate can prove it fixed.
title: ""
labels: ""
---

<!--
Two rules, and everything below is only their shape:

1. THE REPLICATION IS SOMETHING A STRANGER CAN RUN. Not a path on your machine.
   Not a private store, a worktree, a scratchpad, a `do.log`, a `/tmp` anything.
   Whoever picks this up has none of them, and a bug that can only be seen on
   your box is a bug that gets closed unfixed.
2. THE ACCEPTANCE IS END-TO-END FOR BEHAVIOUR THAT ONLY SHOWS END TO END.
   A unit test around a wedge that only appears through the real door proves the
   unit, not the fix. Name the door.

Delete these comments as you fill it in. Issues #185 and #207 are the worked
examples; open either one if a heading here is not obvious.
-->

## What happened

<!-- Quote the log or the screen — its own words, in a fenced block. Say the
date, the base sha (`dev@1140a075`), and the exact command with every flag and
model on it. "It hangs sometimes" is not an observation; the line the run
printed while it hung is. -->

## Replication

**Deterministic (no model).**

<!-- The one a reviewer can run at their desk this minute: a stub worker, a
fixture, `go test -tags e2e`, `aforge do --json` with a short `--timeout`.
Say what a developer sees TODAY when they run it — the wrong output is the
evidence, so quote it — and where it is driven from. -->

**Field (real models).**

<!-- The full-fat version: the real command, which keys it needs
(`OPENROUTER_API_KEY`), and roughly how much wall time and money to budget. Say
what makes it fire — "any task whose first attempt fails" — and name the control
run if you have one. -->

<!-- If the forensics only exist on your machine, they get ONE line and no more:
*Owner's forensics, may be gone:* the run's private store and log were kept on
the benchmark box. Then say whether anything in them is actually needed, or
whether the replication above stands in for it. Never make it the replication. -->

## Where

<!-- File and function names — `settlementWatch` in `cmd/aforge/do.go`, the
escalation site in `cmd/aforge/chat.go` (search `escalated from`). Not line
numbers: they move, and a stale one sends the next reader to the wrong code.
Give the searchable string. -->

## The fix

<!-- What the behaviour should be, in the product's own words, not a patch.
If a flag or a person-facing string is involved, spell it exactly as it will
read. If there is a choice to make, make it and say why. -->

## Acceptance

<!-- e2e FIRST, and each line says what is asserted, not that a test exists. -->

- **e2e:** <!-- through the real door — `aforge do --json`, the tmux suite,
  `go test -tags e2e ./internal/e2e/` — with the exact text or receipt field
  asserted, and the exit code. One test, no model where a stub will do. -->
- **e2e:** <!-- the second case, usually the control: the same run without the
  flag, or the path that must keep working. Delete if there is genuinely one. -->
- **Unit:** <!-- the part that is a pure function or a structural law — a
  derived bound, "every path that assigns X goes through this one check". -->
- The manual page that covers this quotes the new wording
  (`internal/manual/chat/`), and the change entry's `invalidates` names what
  people believed before.

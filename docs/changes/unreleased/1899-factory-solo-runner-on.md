---
kind: changed
title: This machine's factory runs its jobs here from the start, and starts nothing by itself
pr: 1899
surface: [engine, docs]
invalidates:
  - "Was true: the app's own factory served quietly, with no runner, poll, triage or start rule, until a person turned the runner on with `factory.local_run` (Run on this Mac); that one switch was also the consent to the repository's start rules and to triage's model reads, so from then on a job could start without a person. Is true now: once the factory knows whose GitHub it is, the engine turns its runner on by itself (on `factory.local_start`, a resume, a setup step and `factory.local_status`), with the GitHub poll beside it. No start rule launches a job and no triage reads an item in this machine's factory: an arriving issue starts nothing, and a job starts only through `factory.job_start`. `factory.local_run` is the same start, asked for; it still says when GitHub is not connected."
---

A person who set up a factory on their own Mac pressed Run on their first job
and watched it sit in the queue, because the runner was a separate switch in
Settings. The switch existed to ask consent for things that start or spend on
their own; with none of those left in this machine's factory, there is nothing
to consent to, and the runner is simply on. A terminal's `codeaf factory serve`
is unchanged.

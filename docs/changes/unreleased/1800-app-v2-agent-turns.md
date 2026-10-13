---
kind: added
title: The app engine serves agent turns and full snapshots
pr: 1800
surface: [engine, remote, docs]
invalidates:
  - "The app's nine turn routes returned 501. Send, Steer, Queue, Edit, Remove, Stop, Retry, Switch model, task handoff, entries and tool output now have guarded handlers."
  - "The agent snapshot exposed Go session structs and omitted pending messages and asks. It now uses the app contract with entries, durable queue, live open asks, task summaries and usage."
  - "The bridge assumed editable follow-up methods existed on the canonical wire. They do not; this binary retains pending messages until admission, making edit/remove and send one atomic boundary."
---

A4b ports the bridge core without places or app-owned model calls. Pending
messages and anchored receipts survive restart; late stream events cannot
replace a stopped or fresh reply. Running-long events use an injectable timer;
explicit handoff interrupts only the reply and starts the engine's canonical
task. Failed admission leaves a failed reply, rather than inventing a task id.
Attachments use SubmitFiles; full tool stubs are restricted to their transcript.
The canonical provider retry policy remains unchanged, including its variable
patience rather than the design's fixed three-call ceiling. File browsing
remains reserved for a separate resource implementation.

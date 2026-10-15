---
kind: fixed
title: codeaf app hides lines meant for the model, names what each ask is about, and keeps small renames quick
pr: 0
surface: [engine]
---

Was true: a reply resumed after quit, a woken turn's note, a checkpoint's
question and a `[no change]` reply showed in the app as notices or answers; an
ask carried no subject, so an automation's card looked like any question; a
quick task asked to become a full task before any shell call, read-only ones
included, and app agents on the run road were never offered `quick_task`.

Is true now: a session-authored line to the model is marked (`ForModel` on
the display entry) and never becomes an app entry, and a reply that is only
the withdrawal token is never drawn, live or reread; a resumed reply is marked
by the footer `resumed_after_quit`. Asks carry `subject: {kind, ref}`
(automation, task, call, run, page, account, crew_proposal, crew_decision,
budget). A read-only shell command (internal/approval's ReadOnly) stays inside
a quick task, and the app's launch keeps `quick_task` beside runs.

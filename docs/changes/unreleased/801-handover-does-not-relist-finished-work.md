---
kind: fixed
title: a handover no longer lists finished work as work that is left
pr: 801
surface: [engine, chat]
invalidates:
  - "`checkpointSketchAsk` ended at \"Work you have already handed out is not a part\" and said nothing about work the conversation had already finished, so the second reader drew reads and runs it could see completed in the account above it. It now ends with a second exclusion in the same voice: \"Work that is already done is not a part either: what the account above shows finished is not what remains, and drawing it sends somebody to do it a second time.\" The measured wording of the rest is untouched and still pinned by `TestTheSketchAskIsTheMeasuredWording`."
  - "`checkpointHandoffWriteAsk` named four things the handoff carries and left the line between the first two — what is left, and what is already known — for the model to guess. It now states it: \"Nothing that is already done goes under what is left to do: what has already been read, run or found out is what they already know, and putting it under what is left sends them to do it again.\", and that clause sits between the four things and the closing instruction about the person's own words."
  - "`admissionEvidenceRule` described the calls-already-run section without ranking it against anything. It now settles the disagreement it was losing: the section is what has already happened, and where the parts or the brief read as though one of those calls were still to be made, the task is told it has been made already and to read it through its pointer instead of running it again — running it again only where the line says it FAILED or where what it reads disagrees with the brief. THE BODY-ABSENCE LAW IS UNCHANGED: no result bytes were added to a handle, and the entry room inside the 5,000-byte `admissionBudget` follows the rule's length automatically through `admissionOverhead` (3,707 bytes, from 4,030)."
  - "`taskCopy` bound one folder — the ground — so a brief written by a conversation that owns its own workspace named `<session>/work/…`, which is not under the ground, was left verbatim and then refused by `taskGroundGuard` as \"outside your copy\" (#566 fixed the ground, not this). `newTaskCopy` now takes a third folder, the session's own `work`, and binds it to the same path under the copy (`<session>/trees/1/work/…`); the copy section says so outright, and the folder is dropped when the session has none, when it is not owned, or when it already sits under the ground or the copy. PERSON-QUOTED PATHS ARE STILL VERBATIM: `composeBrief` binds only the model-authored sections and has never touched the quoted request."
  - "`internal/manual/chat/tasks.md` said a sketch naming work you have already done was NOT covered. It is now covered as far as the ask can cover it, and the page says which half of the brief wins and what remains uncovered (a part that lands in the seconds after the account was taken). `internal/manual/chat/how-tasks-run.md` said a path not under the project is left exactly as written, with no exception; the conversation's own folder is now that exception, and the page states it with the before-and-after address."
---

Measured on 2026-09-10, chat conversation `850b0c99916083d0`. The conversation hit
its checkpoint ceiling, split the rest into a sketch and wrote a 2,641-token
handoff. The document the worker opened contradicted itself: the machine-composed
`WHAT IS LEFT, AS PARTS:` line at the top named reading two files as remaining
work, while the calls-already-run section further down named the very same reads,
with their pointers, as done. The worker obeyed the louder and earlier half, read
them again, and re-derived conclusions the handoff had already stated.

The same brief also named `<session>/work/task-creation-flow.md` as its
deliverable — the folder `deliverablesDir` answers with for a conversation that
owns its workspace. That folder is outside the ground, so nothing rewrote it, the
first write there was refused, and the worker had to invent an address inside its
own copy and report the deviation. That is now the address the brief hands it.

The diagnosis this is NOT: the worker was short of context. It had the handoff,
the evidence and the quotes. What it had was a document that said two different
things about the same work, and no rule for which one to believe.

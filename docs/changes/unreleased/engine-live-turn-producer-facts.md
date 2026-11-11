---
kind: added
title: typed tool-result provenance and skills or memory notices
surface: [engine]
invalidates:
  - "Was true: nothing separated a tool call refused for its arguments from one that ran and failed, so a surface could only read `Invalid arguments` out of the output text. Is true now: `session.Event.RejectedBeforeRun` (EventToolFailed), `DisplayEntry.ToolResult.RejectedBeforeRun()` and the app's `rejected_before_run` are set only by a handler's typed validation return, journaled on the actual tool-result message occurrence, independently of reused provider call IDs, and a command or request that ran and printed the same words does not carry it."
  - "Was true: replay dropped distinct handler invocation and engine or permission refusal facts. Is true now: true-only `invoked`, `harness_made` and `refused` follow the exact result message; an absent legacy fact remains unknown, a validator can be invoked and rejected, and a task gate that never entered a handler does not claim invocation."
  - "Was true: a skills or memory notice was an ordinary quiet notice whose kind could only be guessed from its words. Is true now: `session.Event.NoticeKind` is `skills` or `memory` at those two producers, and the app's notice entry carries `notice_kind`, `skills` and `unrecorded`; every other notice stays unclassified and memory notices are still not in any transcript record."
---
Engine facts only; the app and v3 surfaces that draw them are separate changes.

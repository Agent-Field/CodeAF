---
kind: fixed
title: an errand's ending names the gate that judged it, and one reader decides a task's audit posture
pr: 646
surface: [engine, chat, docs]
invalidates:
  - "`task.audit` was believed to be the audit row every door reads, and `cmd/aforge/chatv3.go` held the only line in the binary that answers it — so an `aforge do` errand looked like a road where the row had silently failed to arrive. It is one road's row and always was: it governs tasks the conversation hands out with `/task`, it reaches `internal/session` through `applyV3Governance` and nowhere else, and both `session.Config` values built anywhere under `cmd/` pass through that one seam. No road into the task engine ever carried a zero-value posture, and `nothing checked this work: the task.audit setting is off` is never printed where the setting is not the reason."
  - "An errand was believed to run with no check harvest at all, because `internal/session`'s audit door (`auditDoorFor`) never opens on that road. It never opens because `aforge do` builds no `session.Config`: it runs a brain over the resident runner, whose delivery is judged by `revision.JudgeDeliverable`, which harvests the project's own checks through `internal/verify` — a reading before the work and another the gate takes itself — and journals the mapping of what was asked onto the checks that exercise it. Two doors, two gates, and the errand's is not the weaker one; what it lacked was a name."
  - "`aforge do --json` had no field naming what read the delivery, so a settled envelope with no `unjudged` key could not be told from one nothing had checked — `unjudged` appears only where the gate was ASKED and could not be reached, which is one of three ways a run can end with no verdict. It carries `judged_by` now, holding the gate's own name on exactly the runs something judged. Its presence and `unjudged`'s are the two sides of one question and never appear on one object, so a caller reads a key rather than a sentence. A job the planner broke into several pieces journals its gate against the piece that delivered rather than the root that settles them, so the key is absent there and the manual says so."
  - "Nothing said where a run's audit posture comes from, which is the whole reason the question could be asked. `cmd/aforge/taskaudit_law_test.go` reads the `cmd` tree with `go/ast` and holds three things: `TaskAudit` is assigned once, `config.TaskAuditEnabledAt` is read once, and every non-test file that BUILDS a `session.Config` — a literal that sets a field, or a `var` filled in afterwards, never the zero value handed back on an error road — reaches `applyV3Governance`. The roster is the tree, so a door written tomorrow joins the law without anybody remembering to add its filename."
  - "`revision.GateName` is the one Go spelling of `delivery gate`, beside `GateUnreached` and for the reason stated there. The envelope, the `docs/HEADLESS.md` contract table and the chat manual are three readers of one string."
---

The issue asked a question before it asked for a patch, and the answer turned out
to be that the machinery was right and only unnameable. An errand's delivery has
always been read — the project's own checks are photographed before the work and
measured again at the gate — but a reviewer holding the `--json` object had no
field naming either that gate or the row that governs the other road, so the
honest reading of a clean envelope was that perhaps nothing had checked it.

Nothing here spends anything: `judged_by` is a read of a row the run had already
written. What cost something was the silence around the one line that decides the
other road's posture, and that is now a law rather than a thing to be
rediscovered.

Review correction: the receipt and manual distinguish an answered gate attempt from
a successful check. An unreadable answer still names the gate, while missing root
rows (including split jobs) can omit both optional keys.

The current-dev integration preserves both the checklist and reader fields,
including the latest finished-verification journal ordering and its incomplete
tree ending. The independent records coexist in the same JSON response.

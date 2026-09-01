---
kind: changed
title: the write-your-notes rule is held rather than advised, and a turn that will not comply stops
pr: 154
surface: [chat, engine]
invalidates:
  - "A `[silent]` note never stopped anything, and the manual said so outright. The second one is now enforced: past twelve tool-using replies with no visible text, the next reply that carries only tool calls is answered `[held] Nothing was run this step…` in place of every result and none of its calls is run."
  - "The second `[silent]` note said `This is the second warning; stop calling tools until you have written that note` with nothing behind it — it fired thirteen times in one measured conversation and stopped nothing. It now says `from here your tool calls are held` and that is true."
  - "The silent ladder had three rungs, at six, twelve and twenty-four replies. It has two. The hold freezes the streak at twelve so nothing could ever reach twenty-four, and the third rung's words (`Nothing is being stopped — keep working`) had become the opposite of what happens."
  - "A turn could not be ended for being quiet by any road. It can be ended by ONE: after three held replies it lands on `stopped here · would not write its notes down, so what this turn worked out is not on the record`. Nothing is handed to a task, and this is never written up as `went in circles`."
  - "The only loop-enforceable rule was the loop guard's own repetition ladder. There is now a registry of process rules the turn loop holds a model to (`internal/session/processrule.go`) — a rule answers four questions and the loop knows nothing about notes — with write-your-notes as its first tenant."
  - "How often a nudge had to be given could only be reconstructed by grepping a transcript for a bracketed word. The session journal carries a `rule` line — `advised`, `held`, `stopped` — with the conversation's running advisory count on every one of them."
  - "`prompts/system.md` numbered its Workflow sections 1–6 and stated `plan before files` under `## 1. Scope`. Scope is gone as a section, its law folded into `## 2. Decompose` where the same rule already stood, and the sections are numbered 1–5."
---

The advisory fired thirty-two times in one dogfood conversation — nineteen at six
silent replies and thirteen more at twelve — and was obeyed approximately never.
A rule the loop can enforce is not a suggestion: the harness owns the turn loop,
so when a process rule matters to the record and the advisory is demonstrably
ignored, the loop stops running the next submission until the rule is met. The
advisory step itself is unchanged, so a model that writes its notes as it works
cannot tell any of this exists.

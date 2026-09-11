---
kind: changed
title: A harness message says how to read itself, so the page stopped explaining messages
pr: 812
surface: [chat]
invalidates:
  - "The page taught the four landing words. `prompts/system.md` no longer says them: a landed task's note opens on its own lead, which carries the tier word and tells the model to say that word back and answer the request the work was for."
  - "The page explained the woken turn and the `[carry on]` line. Both paragraphs are gone from `# Interrupts and steering`; `checkpointCarryOnLead` has always carried its own half, and a landing note now carries the other."
  - "The belt's `tasks` fact repeated the four words and the resolve verbs. It does not: `settleClause` names the address with its verbs interpolated from `TaskResolutions`, a clash with the person's branch is refused by `conflictNotYours`/`shiftNotYours` where it happens, and a closed graph is refused in the tool's own reply."
  - "The page carried a bullet saying that telling a task to stop does not stop it. It is gone: the `tasks` description owns that law, in the sentence beginning `To END running work use stop`."
  - "A job's exit note said only `job 3 exited 0` and a model could read it as somebody typing that line. It ends with one clause saying it is news and asks nothing."
---

Three delivery classes were competing for one law. `docs/design/prompt-diet/DESIGN.md`
§2 files what to do about an event WITH THE EVENT: it is needed only on the turn it
happens, so it rides the message that announces it and costs nothing on the thousands
of turns where no task lands and no job ends. `standingNewsRule` and
`checkpointCarryOnLead` were already written that way; the landing note and the job
exit note are now too, and the paragraphs the page spent restating them are deleted.

The prefix went 47,435 → 45,246 bytes, all of it out of the page (23,391 → 21,202).
The budget was not raised. Nothing about a landing left the build: the manual's
`## The four words a task can land with`, `## Why is the task waiting for me — what
does your call mean` and `## How do I accept a task` are where a person's question
about any of it is answered.

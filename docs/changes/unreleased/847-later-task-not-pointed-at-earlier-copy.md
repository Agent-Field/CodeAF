---
kind: fixed
title: a later task is not pointed at an earlier task's copy
pr: 847
surface: [engine, chat]
invalidates:
  - "`taskCopy.bind` moved two folders — the node's ground (#566) and the conversation's own `work/` (#804) — so a contract that named an EARLIER TREE of the same conversation (`<session>/trees/1/…`) was left exactly as written, and the worker standing in `<session>/trees/2` was refused `is outside your copy` about a directory the harness itself had invented. `taskCopyFor` now reads `Place.Trees()` from the same record the copy was cut from and binds another tree of this conversation to the same path under this copy. The ground rule still wins where both could apply, an address already inside this worker's own copy stands as written, and a path that is genuinely somewhere else on the machine is untouched and still earns the refusal — `taskoutside.go` is unchanged, and #566's guard is not widened. The person's own quoted words are still never rewritten."
  - "A contract whose only address was under another tree of this conversation named no file under the ground as far as `groundNamesWorkUnder` could see, so `groundMode` made the work a REFERENCE — a folder that is deliberately not a copy of its ground, and therefore binds nothing. Naming a file in another tree of this conversation is now naming work under the ground, so such a task is given a branch of the person's repository like any other. The node's provenance does not move: its ground stays the person's repository and its copy stays `trees/<its own id>`."
  - "`internal/manual/chat/how-tasks-run.md` said a path that is not under the project is left exactly as written, with the conversation's own `work` folder as the one exception. Another task's copy under this conversation's `trees/` is an exception too, and the page states it with the before-and-after address under the heading \"A later task is not pointed at an earlier task's copy\"."
---

Issue #839, found by the whole tagged e2e package — where it fired once in five
runs of `TestAWorktreeTaskFollowsItsContractInsideItsOwnCopy` while the subtest
alone always passed — and then pinned deterministically by
`TestASecondTaskIsNotPointedAtTheFirstTasksCopy`, which cuts two trees for one
conversation in the order the package cuts them. The conversation learns the
address of a task's private copy honestly, from the first worker's own report,
so the model that proposes the next piece of work writes that address into WHAT
TO PRODUCE; what was wrong was the handoff, not the model and not the guard.

---
kind: fixed
title: A quick task comes through one door, survives its checkpoint, and holds the files it wrote
pr: 879
surface: [chat, engine, docs]
invalidates:
  - "A checkpoint that held a quick node was refused whole (`ignoring corrupt task checkpoint`, node N has no acceptance), so a conversation that had run `quick_task` resumed with no tasks at all. The decoder now accepts an empty done-condition for kind `quick`, and only for that kind."
  - "`taskRecord` carried a quick node's kind and not its body, so a restored quick node had `spec.quick == nil` and its ticks were lost. The record now carries `quick: {line, items, done, files}`, a tick checkpoints, and `restoreNode` rebuilds the body."
  - "A quick node was settled on restart only if it was running, and it was counted as `interrupted (no branch kept)`. A quick node that was running or still queued now settles as `failed`, its report lists which items were ticked and which were not, and the recovery line counts it as `N quick tasks did not finish`."
  - "`continue task N` re-queued a quick task, and a restored one then ran as an ordinary worker in a worktree. It is now refused with `task N is quick, not a run that can be continued`."
  - "The ceiling's handover admitted its quick node through `launchRouteTask(…, quick)`, with an invented acceptance, a worktree mode, a naming call and depth 0. Every quick node now comes through `Agent.admitQuick(quickAsk)`, and `launchRouteTask` no longer takes a quick parameter."
  - "`fileOwner` held written files only for nodes with a branch, so a running quick task held nothing and the chat could overwrite a file it had half-written. Every running node that does not hold its whole tree (`TaskNode.holdsTreeLocked`) now owns the paths it has written, and a write to one of them is refused with the holder named (`<file> is held by task N (<title>), so nothing was written.`)."
  - "The chat manual said a file a quick task *named* at the start is held against the chat. It is not. Only files it has written are held; a named file only makes a second quick task that names the same file wait."
  - "`PresenceTask` had no kind or parent, and `<elsewhere>` listed another window's quick parts as separate rows. It now carries `kind` and `parent`, plus `done`/`total` taken from a quick task's items, and the block folds the parts onto their head row (`3 quick parts running`)."
  - "`TaskNode.Expects` was never written to the checkpoint. It is written now: the brief section is always restored, and the preflight manifest is restored only onto a node that never ran."
---

Found by the task-start wave on the owner's own `tasks.json`. The two doors had
built two different objects, and the record could name a quick node but could
not rebuild it. The fix makes the kind decide the shape in one place, and makes
the record carry the list.

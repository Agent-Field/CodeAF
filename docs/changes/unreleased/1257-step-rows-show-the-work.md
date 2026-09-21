---
kind: changed
title: a run's step rows show the work and leave out the run's own bookkeeping
pr: 1257
surface: [chat, engine]
invalidates:
  - "Every step a run's worker recorded was drawn on its task page as typed, so most rows began with a change into the run's own copy and a third of them were the run updating its own record. A step row now leaves out a leading change into the run's own copy and any part addressed only to the run's record, with whatever that part is piped through. A step with nothing else in it has no row."
  - "Every other byte is drawn as it ran. Parts carry the span they were typed in (`PlanCommandPart.Start`, `End`, `SepEnd`) and the surface CUTS left-out spans from the recorded command; it never rejoins trimmed parts. A command with nothing left out is drawn whole. `PlanStep.Command` is unchanged and is still the record."
  - "A row keeps the number its step ran as, so the numbers can skip: a page headed `12 steps` may draw rows `1` to `4`, then `9`."
  - "There is ONE reader of where a command ends and the next begins, in `internal/approval` (`SplitBashCommand`, which the allow-rule reader `splitSegments` now sits on). The session asks it; nothing else may split a command."
  - "The parts cross the wire (`PlanStep.Parts`, `PlanTaskRow.LiveParts`), so a hosted conversation draws the same rows a local one does. The run's copy is the belt run's own (`beltRun.workspace`), and a folder directly under the conversation's folder of copies counts after the run has ended and the copy is given back. `PlanTaskRow.Folder` keeps its meaning."
  - "A part is marked only where cutting is safe: in sequence with its neighbours, never inside `$( )` or a group, and never when work is piped INTO a record command."
---

Two cells built this and neither brought it home: the first hit the rig's wall
with its commits in the run's cut copy, the second its checker cap. Both sets
were recovered by hand. The hosted review on the real binary found that the
surface retyped the command from trimmed parts, which the span cut replaces.

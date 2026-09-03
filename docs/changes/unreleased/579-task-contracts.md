---
kind: fixed
title: a task's contract, and a division's checks, are written where the work actually happens
pr: 579
surface: [engine, chat]
invalidates:
  - >-
    An absolute project path written into a handed-off contract used to reach the
    worker unchanged, and was safe for it to follow. It is not, and never was: a
    worktree or mirror task stands in <session>/trees/<id>, so an address naming
    the person's checkout read a directory that was still moving under it and the
    first write at it was refused as "is outside your copy". Every address at or
    below the ground in the model-authored half of a brief — the work, what to
    produce, what done means, what the handoff assumed — is now rewritten as the
    same address inside the copy before the worker is asked anything.
  - >-
    The task manual said a task writes only in its own copy and left the reader
    to assume a brief naming the project by its full path was a brief the task
    could not follow. It can: how-tasks-run.md now states that a contract path is
    resolved inside the copy, that the copy is what the task reads rather than
    the changing original, and that the person's own words are quoted unchanged
    with both folders named beside them.
  - "Independent private copies were taken to justify repeating one family-wide done-condition in every part — each part has a worktree of its own, so the thinking went, and each may as well prove the whole family green before it comes home. No longer true. The parts run at the same time and none of them can see what the others wrote, so a check copied into three parts is three concurrent runs that judge a tree without their siblings' files in it, plus the parent's own run afterwards: four runs of an eight-minute suite where the work needed one, and only the last of them able to be right."
  - "A division was admitted whenever no two parts claimed the same PATH. It is now also refused when one COMMAND stands in two or more parts' done-conditions — asked at the same two moments as the ownership rule, before anything is paid for and again on the parts the mastermind settled — and the worker is told the command by name."
  - "`refused:scope` was the only record of a division that was right with the wrong parts. There are two more words — `refused:shared-check` and `repaired:shared-check` — and the division line carries a `shared` field naming the commands more than one part was ordered to run."
  - "A division whose parts repeat a check was taken to be refused every time it was put. No longer true, and the belief cost a measured run: on a cheap model the divider re-asked with the same shape four times, collected four refusals and ended `stopped: 6 steps without progress` with all three parts' files already written. The FIRST telling on a node still refuses, so a worker that can redraw its done-conditions does; a SECOND firing of the same rule on the same node ADMITS the division with the shared command removed from EVERY part — never left on the first — and the record says `repaired:shared-check` rather than `admitted`."
  - "A node carried no checks of its own beyond the acceptance it was admitted with. It now has `TaskNode.Family`: the checks a division lifted off its parts, which the parent is told about under its own DONE WHEN and which its own checking door will run. Nothing writes a spec after admission — that law is unchanged and still pinned — so these are a field the node carries beside the spec, not an edit to it. `Family` is on the checkpoint (the parent's run happens after every part is home, which can be another process); the once-only telling behind it is NOT, exactly as `adjudicated` is not."
  - "Nothing in internal/session answered whether two spellings of one check are the same check — `appendChecks` dedupes on exact bytes, `commandLike` reads shape, `runnableHere` reads whether a span could run at all. `normalizedCheckCommand` in task_checks.go is now that one reading: whitespace, a trailing path separator and a `-count=1` are dropped, and every word that narrows what runs is kept, so `./internal/tui3` and `./internal/tui3/...` stay two different commands."
---

## The contract and the copy (#566)

Issue #566, reproduced with nothing scripted but the model. A parent standing in the
person's checkout writes that checkout's absolute paths into `brief`, `deliverable` and
`acceptance` — which is exactly what prompts/system.md asks it for — while the node it
hands them to is stood up in a private worktree. The isolation was right and
`taskGroundGuard`'s refusal was right; the handoff was wrong, and this is upstream of both.

The map from the folder the work is ABOUT onto the folder the work HAPPENS IN is applied at
the only two moments a worker is spoken to: its opening request, and every repair round. A
path that is NOT at or below the ground is left exactly as written — that is what keeps the
guard honest, since nothing outside the copy can be rewritten into something writable. The
person's own sentence is a quotation and is never edited; where the folder was named at all
the brief states the two directories instead, once, before the first address it explains.
A reference task binds nothing, because its folder is deliberately not a copy of its ground.
Nested parts are fixed at the same time by the same method: a part's tree is cut off the
parent worker's directory and reaches its worker down the identical road.

## The check every part was given (#569)

The refusal is not a finding that the work cannot be split — that is what the floor
gates say, and it is a different finding. It is a finding that these particular
done-conditions are wrong, so it names the shared command, asks for the check that
proves each part's own slice, tells the worker to keep the whole run in its own
done-condition and make it once after the parts' work is home, and asks it to come
back. The commonest case costs nothing at all: the reading stands above the line
that spends money, exactly as the ownership rule does.

It classifies by REPETITION ACROSS SIBLINGS and by nothing else. There is no build
system named anywhere in it and no threshold in seconds, because either would be a
list to keep up to date and neither is the fact that matters: two genuinely targeted
checks differ in what they name and stay two checks, and a family-wide check is the
same words in every part — which is exactly what makes it nobody's.

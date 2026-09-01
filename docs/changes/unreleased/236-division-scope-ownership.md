---
kind: changed
title: no two parts of one division may own the same path, and it is refused at admission
pr: 236
surface: [engine]
invalidates:
  - "Overlapping scope between the parts of a division was advice a worker could ignore — prompts/divide.md said \"Two parts that edit the same file are not independent\" and the reviewer's brief asked it to fix the boundary. It is now ENFORCED: a division whose parts name the same path is refused before any part exists, and the worker is told which path."
  - "The reviewer's brief used to tell it to name, in each part's brief, \"what NOT to touch because another part owns it\". It now asks it to say WHOSE something nearby is rather than listing a sibling's files under this part, because the second reads as a claim."
  - "journalDivision's Decision could be refused:arguments, refused:floor, refused:review-unreached, refused:lane, refused:cap, refused:review or refused:nobody. There is now an eighth, refused:scope, and a bench counting refusals has to know it."
---

The ledger is the contract of what ships and `stageTaskWork` stages it once, so
two parts of one division writing the same path put it in the parent's ledger
twice and one version is quietly kept. Nothing conflicts, nothing is reported,
and the other part's work is simply gone. That is not a merge to resolve; it is
a scope bug, and it is now answered in `divideOnce` — twice, through one
function. The parts the worker wrote are read ABOVE the line that spends
anything, so the commonest overlap is refused for nothing at all and the
sentence may honestly say so; the parts the reviewer settled are read again
before any hand is claimed, because a sharpened brief can land on a file its
sibling already owns, and that refusal says nothing about spend rather than
claiming a reading that was already paid for.

The check is deliberately dim (`internal/session/task_divide_scope.go`): only
the exact same normalised path claimed by two parts collides, so parts sharing a
directory, or a part owning a folder while another owns a file inside it, admit
exactly as they always did. And what a part CLAIMS is its own scope rather than
the family context composed around it — `sketchBrief` writes the parent's whole
brief above every part and its siblings' scopes below, and reading either as a
claim would make that road refuse its own best case.

---
kind: fixed
title: a landing reads the commit its branch was cut from, and not only its name
pr: 626
surface: [engine, chat]
invalidates:
  - "\"moved since the cut\" used to mean the branch NAME and nothing else. #589's own change entry says it out loud — \"committing or rebasing on the same branch you started on is not 'moved' and still merges\" — and `internal/manual/chat/how-tasks-run.md` said \"The test is which branch you are on, not what is on it: commit or rebase all you like on the branch you started on and the work still comes home to it\". Both are false now. The cut records the branch's COMMIT beside its name (`homeSha`, additive on the task record, the standing-tree record and the node), and a landing keeps the task branch when that branch no longer stands where the work was cut from."
  - "A person who committed, amended, rebased, reset or pulled on the branch they started the task on was merged into WITHOUT A WORD. The rebase is the one that cost something: the merge put the commits they had taken off the branch straight back onto it. They now get `its branch task/x was kept: feat/x has moved on since the work was cut — merge it where you want it`, the work finished and waiting on its branch."
  - "aforge's OWN landings are not the person moving their branch, and this is not a refinement — without it the second task of any pair, and four of any crew of five all cut from one tip, would be kept, because the first landing moved the tip. The rule is \"the branch moved, and not by aforge's own hand\": a rewrite is always the person's, and a forward move is theirs when any commit in the new range carries a committer other than aforge's. The COMMITTER and not the author, because a rebase of aforge's own commits keeps aforge as the author."
  - "A conflict from work the person COMMITTED on their own branch is no longer reached in their own repository: that landing is kept before any merge is tried. The conflict road — the clean abandon, the checkout put back as it was, the files named — is now for their UNCOMMITTED work and for the repositories aforge owns, and five tests that made their conflict with a commit moved with the contract."
  - "`/land` takes the same check through the same function. A conversation's own branch on a referred repository is kept when the person moved that repository's branch between the cut and the landing, and the standing-tree record carries the commit across a close and reopen."
  - "The identity `aforge <aforge@localhost>` was spelled out as a literal in seven places. It has one name now and every writer uses it, because the guard that recognises aforge's own commits compares against the same string the writers stamp."
  - "A record written before this field decodes with it empty, keeps the protected, detached and moved-name arms exactly as they were, and merges on a moved tip as it always did. `taskFileVersion` is still 1 and a record with nothing to write is byte-identical to what it wrote before."
---

#589 promised a landing is refused when the person's checkout "moved since the
cut" and compared branch names, because nothing recorded where the branch stood.
This is the commit half, and it is the half that makes the promise true: the name
answers which destination somebody chose, and only the commit answers whether
that destination is still the world the work was cut from.

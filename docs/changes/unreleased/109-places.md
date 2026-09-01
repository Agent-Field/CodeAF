---
kind: added
title: a conversation can be about places beyond the one it opened in, and work there lands as a diff
pr: 109
surface: [chat, engine, build]
invalidates:
  - "furrow was optional, absent-not-broken, installed by the person; it is now embedded in bin/aforge (go:embed, extracted version-stamped under the state root), every build carries it, and the SIZE-BUDGET ratchet was raised for it by owner ruling 2026-08-31."
  - "a conversation had exactly one directory, immutable after launch, and no picker existed anywhere; there is now a referred-places set on the conversation (feeding the SAID rung of the task ground ladder, never a second resolver) and a /folder picker that is also the forming card's g correction."
  - "/attach refused a directory with 'is a folder · attach a file'; a directory is now the door to referring a place."
  - "the conversation's own read/write/edit reached exactly one directory, and the manual said choosing a folder 'does not make edits land somewhere else'; a write aimed at a referred folder now goes into a working copy of it — a git worktree off its HEAD, or a clonefile/copy for a plain folder — cut lazily on the first write, with reads of files in that copy answered from it, and /land (then /land now) puts the work into the folder. bash is NOT redirected and the manual says so."
---

The wave for issue #108: the window is where you sat down; the conversation is about
places. Referred places accrue from evidence (a named path, @ on a directory, a folder
drop, a kept ground resolution) and are shown as consequences — a ground row, a
changes-waiting chip — never as an inventory. Writes aimed at a referred place go
through a standing tree and land only when the person has been shown the file list and
said so; the standing place stays direct, and a folder somebody said "in place" about is
written directly too. The tree is a git worktree for a repository and a clonefile copy for
a plain folder, landed through the roads #90 already built (comeHome, landMirror). Furrow
is embedded, and a byte-exact universe as the third road is a named seam in
standingtree.go rather than something this wave ships: internal/furrow materializes a
universe only as the side effect of running a command in one.

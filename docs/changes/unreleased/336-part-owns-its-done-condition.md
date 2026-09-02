---
kind: fixed
title: a part of a division owns what its done-condition names, not what its brief mentions
pr: 336
surface: [engine]
invalidates:
  - "The scope check landed by #231 read a part's brief AND its done-condition as claims, so any exact path two briefs both named was refused as an overlap. It reads the done-condition only: a brief names the material a part works on — everything it reads included — and a file two briefs both mention is nobody's claim."
  - "Two parts told to read one shared file and write a file each could not be admitted at all; a folder ground seeded with a README.md produced `README.md, a.md, b.md, c.md are each claimed by more than one part` three times over and the worker gave up dividing. That division is admitted now, and only two done-conditions naming one path are refused."
  - "`internal/e2e/families_e2e_test.go`'s `newFolderGround` was deliberately EMPTY and said so in a comment, because anything seeded there was named in every brief. It seeds README.md — the material every part reads — and the folder scenarios ask for shared reading on purpose."
  - "`divide_work`'s `brief` field said \"WHAT THIS PART OWNS\" and its `acceptance` field said only \"DONE WHEN\". The brief now says \"WHAT THIS PART WORKS ON\" and that material several parts read is fine to name there; the done-condition says it is where ownership is read and must name everything the part produces and nothing it merely reads. prompts/divide.md and the division reviewer's brief teach the same split."
  - "The refusal ended \"say in its brief which ones it owns\". It ends \"say in each part's done-condition which ones it produces\", which is the sentence the check actually reads."
---

The check stays as dim as #231 made it and gains no grammar over the prose: what
changed is WHICH SENTENCE is read for a claim, not how it is read. The hard floor
is unmoved — two parts whose done-conditions name one path are refused before
anything is claimed, started or paid for, and the refusal names the path.

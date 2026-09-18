---
kind: added
title: A non-verified `codeaf exec` or `codeaf run` run names its kept branch and its verdict
pr: 1186
surface: [engine]
invalidates:
  - "`codeaf exec --json` and `codeaf run --json` said how a run stopped and named no branch and no verdict, while `codeaf do --json` has carried `kept_branch` and `verdict` on its non-verified ends since #1182. Both doors now carry the same two keys, in the same words, so a caller reading one object shape across all three verbs reads the same two facts on all of them."
---

An exec run and a run-door run end the same way a `do` errand can end: with work
on disk that nothing has judged. Both doors work in place in the directory they
were pointed at, own no session graph, and leave a pending judge record with
`State: session.TaskUnverified` for the Model Pool's restart-time sweep to score —
so until that sweep reads it, the work is standing in the workspace and no
envelope field said where or with what verdict. That is the gap this closes, in
the vocabulary #1182/#1184 already use — `session.TaskFailed`/`TaskUnverified`,
no new words.

**Where the two keys come from.** The merge lives at the one seam all three
verbs build their envelope through (`buildResultEnvelope` over `runResult`,
`cmd/codeaf/envelope.go`), so the keys cannot drift apart between doors; `do`'s
own emission moved onto the same seam unchanged. `exec`'s word is decided by
`execVerdict` (`cmd/codeaf/exec.go`) off the same condition the pending landing
uses: a run the landing refuses — never started, or broke with no text and no
artifacts — says `failed`; everything else that ran says `unverified`. The run
door's word is decided by which ending it left through (`cmd/codeaf/
subharness_run.go`): `sayEnvelope` is `unverified` on both of its endings (the
landing is written whatever the stop), `sayFailedEnvelope` is `failed`, that
road writing no landing at all. `kept_branch` is the workspace's own branch,
read once the run is over by `keptBranchIn` (extracted from `do`'s
`errandKeptBranch`, `cmd/codeaf/do.go`): empty for a detached HEAD or a
workspace that is not a repository, and the key absent with it — while the
verdict still stands, because the word does not depend on git.

Both ride the omitempty spirit of #1182's emission: a run that names no verdict
carries neither key, so the presence of either is itself the answer to "was this
work landed?". Nothing else about either envelope moved.

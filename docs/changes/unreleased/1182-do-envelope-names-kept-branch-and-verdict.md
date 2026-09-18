---
kind: added
title: A non-verified `codeaf do` run names its kept branch and its verdict
pr: 1182
surface: [engine]
invalidates:
  - "`codeaf do --json` said how a run stopped and named no branch and no verdict for a run that did not settle whole. The envelope now carries `kept_branch`, the branch the errand's own work is standing on, and `verdict`, the record's own word for what left it there, on those runs."
---

A run that ends without the gate's approval keeps its work rather than throwing
it away — "not proven" is not "throw it away", and it is not "land it either".
The work is real and recoverable; it simply has not come home. Until this change
the `--json` envelope said only *how* the run stopped, so a machine reading it
could see the work had not landed and had no field naming where it was or why it
was left there.

**Where the envelope came from, and what it carried.** The `do`-specific keys
are built by `legacyErrandFields` (`cmd/codeaf/envelope.go:501`), which turns a
`headlessOutcome` into the field map; the outcome itself is filled at the
settling seam in `settlementWatch.compose` (`cmd/codeaf/do.go:2508`). A
non-verified end set `stop`, `Deliverable` and `Unjudged` there and stopped, so
the envelope carried neither a branch nor a verdict word for the work it would
not land.

`verdict` is the record's own word — `failed` for a node the store settled
failed or cancelled, `unverified` for one that ran and then nothing could say the
work holds — read at the settling seam so it can never disagree with `stop`
(`cmd/codeaf/do.go:2528`, `:2531`). `kept_branch` is the branch the errand's own
work is standing on, read off the workspace once the run is over
(`cmd/codeaf/do.go:708`, `:773`). Both ride the omitempty spirit of `unjudged` and
`judged_by` (`cmd/codeaf/envelope.go:549`, `:552`): a run that settled whole
carries neither, so the presence of either is itself the answer to "was this work
landed?". A workspace that is not a repository, or whose HEAD is detached, names
no branch — there is none a person could check out — and still carries its
verdict, because the word is the record's and does not depend on git.

`codeaf do` works IN PLACE: it edits the directory it was handed, on whichever
branch is checked out there, so that directory's own branch is what `kept_branch`
names here. The `task/<slug>` branch a `/task` node keeps is a property of the
chat door's own task worktree and does not reach this road.

---
kind: fixed
title: reopened rooms keep each call's duration; e2e suite isolation and budget
pr: 814
surface: [chat, engine, docs]
invalidates:
  - "A finished tool call's own duration used to live only on the live EventToolFinished stream. A task room opened after the landing — or rebuilt from the journal — drew the rows with Args and Output and no figure. The journal now writes a `took` line per finished call (keyed by call id, the same anchor the live event carries), DisplayEntry.Took carries it, and replayBlocks puts it on the row so a reopen still says `3.0s` and `7.0s`."
  - "`aforge manual \"…\"` with no key and no model printed `[permissions ·` for the person's section labels. The person's door is `## permissions ·`; the model's bracketed form stays on Render. Both openers are one exported spelling now (PersonSectionOpen / ModelSectionOpen)."
  - "The session suite's post-run checkout guard treated ambient `.aforge-v3/` and live `task/` worktree churn from another aforge on the same box as this run's writes. It now ignores that ambient noise when TMPDIR is outside the repo and another aforge is running, and still watches HEAD and porcelain."
  - "`go test -tags e2e -timeout 40m ./internal/e2e/` was the documented door for the whole tagged package. The package does not fit in forty minutes; `make test-e2e-tui` is TestTUIE2E alone under 40m, and `make test-e2e` is the full package under 120m."
---

Architectural fixes for a cluster of CI/e2e reds that were not owned by the
news/handover/retarget waves: per-call Took persistence (the real roomfeed
defect), person-facing manual labels, hermetic checkout against a live aforge,
and an honest e2e timeout split. Settle/nested-landing and standing-order chrome
needles stay deferred where they still need a live-model run to prove.

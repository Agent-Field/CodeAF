---
kind: fixed
title: roomfeed waits for the room's way back; reopened rooms keep each call's duration
pr: 820
surface: [chat, engine]
invalidates:
  - "The roomfeed e2e twins waited for `room ·` to decide a task room was open. At their 120×40 frame with the rail showing, the organized layout draws `esc/← main` on the focus header and leaves the legend empty — so that needle never fired on a real open page. They wait for `esc/← main` (tuiwords' roomBackWord) now, the one string every room shape shares, opened by a rail click that does not compete with the settle strip's Enter."
  - "A finished tool call's own duration used to live only on the live EventToolFinished stream. A task room opened after the landing — or rebuilt from the journal — drew the rows with Args and Output and no figure. The journal now writes a `took` line per finished call (keyed by call id), DisplayEntry.Took carries it, and replayBlocks puts it on the row so a reopen still says `3.0s` and `7.0s`."
  - "The roomfeed twins waited for those figures on a page that had already folded them behind `▸ worked`, and named the probes with a command long enough that toolTail shed the duration. They open the chip, and the probe commands are short enough that the figures still fit on the row."
  - "The session suite's post-run checkout guard treated ambient `.aforge-v3/` and live `task/` worktree churn from another aforge on the same box as this run's writes. It now ignores that ambient noise when TMPDIR is outside the repo and another aforge is running, and still watches HEAD and porcelain. When TMPDIR points inside the checkout, in-tree `.aforge-v3/` worktrees are still reported — that is the #578 leak."
---

Roomfeed was red for three stacked reasons that looked like one: a moved
room-chrome needle, a journal that dropped per-call duration on reopen, and a
checkout guard that blamed the suite for a neighbour aforge's task trees.

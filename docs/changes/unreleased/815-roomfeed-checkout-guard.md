---
kind: fixed
title: roomfeed waits for the room's way back; the checkout guard ignores a live aforge's task trees
pr: 815
surface: [chat, engine]
invalidates:
  - "The roomfeed e2e twins waited for `room ·` to decide a task room was open. At their 120×40 frame with the rail showing, the organized layout draws `esc/← main` on the focus header and leaves the legend empty — so that needle never fired on a real open page. They wait for `esc/← main` (tuiwords' roomBackWord) now, the one string every room shape shares, opened by a rail click that does not compete with the settle strip's Enter."
  - "The same twins waited for per-call figures on a page that had already folded them behind `▸ worked`, and named the probes with a command long enough that toolTail shed the duration before it reached the screen. They open the chip, and the probe commands are short enough that `3.0s` / `7.0s` still fit on the row."
  - "The session suite's post-run checkout guard treated ambient `.aforge-v3/` and live `task/` worktree churn from another aforge on the same box as this run's writes. It now ignores that ambient noise when TMPDIR is outside the repo and another aforge is running, and still watches HEAD and porcelain. When TMPDIR points inside the checkout, in-tree `.aforge-v3/` worktrees are still reported — that is the #578 leak."
---

The suite was red for interference shapes and moved chrome, not for a missing
`room ·` product string: organized rooms put the way back on the header, and a
shared-box checkout guard could not tell a neighbour's live aforge from a test
that grounded in the developer's tree.

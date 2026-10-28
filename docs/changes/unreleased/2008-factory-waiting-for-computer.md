---
pr: 2008
kind: fixed
title: A queued job on a computer that is down names that computer
surface: [engine]
invalidates:
  - "Was true: a queued job whose work was on an offline or asleep computer read \"Waiting for an available runner\" with advice to change Jobs at once, and Start now did nothing. Is true: it reads \"Waiting for <computer> (offline for 3 h)\" (state_presentation kind waiting_for_computer), and Start now refuses with why."
---

A job whose worktree is on one computer can only run there, so no free slot
elsewhere and no Jobs at once setting starts it while that computer is off.
The queued job now says which computer it waits for and how long that
computer has been offline or asleep, and Start now refuses with the same
reason before writing anything. As with every machine name, only that
computer's own person sees it; anyone else reads "Waiting for its computer".

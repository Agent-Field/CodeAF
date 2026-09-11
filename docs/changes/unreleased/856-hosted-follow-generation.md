---
kind: fixed
title: A hosted conversation keeps drawing the turns the engine starts after this window's own first turn
pr: 856
surface: [chat]
invalidates:
  - "It was believed that a wake — the turn the engine starts when a task lands — always reached the attached window (wakelane.go's fix). It did not once the window had run a turn of its own: watchFollowing carried the TURN generation (app.gen), every typed turn bumps it, so the next Following was discarded as a turn from a connection the window had left and the wait was never re-armed. Every wake after that went to the journal only. The waits now carry app.linkGen, which only a conversation switch bumps."
  - "The keyboard lane (watchDriving / drivingMoved) had the same shape and the same fix; a hand-over after this window's own turn is heard and the wait re-armed."
  - "#855 gave the steer lane its own conversation generation (app.steerGen) for the same defect one lane over. There is now ONE such counter for the three lanes, app.convGen, bumped where a switch replaces the conversation; steerGen and linkGen do not exist."
  - "PR #699 (2026-09-09) was the same fix for the two connection lanes and sat unmerged until it conflicted with dev; its manual paragraph on how-tasks-run.md rides here and the PR is closed as superseded."
  - "This was invisible in the in-process launch, which draws wakes through the local wake lane, and in a scratch home whose path is over the 104-byte unix-socket limit, because such a launch silently falls back to in-process. Reproduce it on a daemon: a short AFORGE_HOME, a quick task that lands, one typed turn first."
---

Seen 2026-09-10 in a conversation on the workspace daemon: three quick tasks
landed, the engine wrote its sentences about them, `whats up` was answered twice
in the journal, and the screen showed a task card and empty turns until the
conversation was reopened.

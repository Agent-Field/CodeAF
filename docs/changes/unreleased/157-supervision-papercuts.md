---
kind: fixed
title: refusals name a door, the composer owns the letters, and the check reads the ground
pr: 157
surface: [chat, engine]
invalidates:
  - "A landed task's room said `task finished — esc to return`, at its foot and in the box's own placeholder. It says `this task has finished — say it to main`, and `this task has finished — say it to main, or open its parent, Ship the port` where the node was spawned under another one. No refusal on the task surfaces names `esc` any more: the legend and the pinned header both already carry it."
  - "A running background job's page said `this log grows as the job works — esc to return`. It says `this log grows as the job works — say it to main`. A session with no task doors said `room unavailable — this session has no task rooms`; it says `this session has no task rooms — say it to main`, because `room unavailable` was the program describing its own wiring."
  - "The roster's widen key was the bare letter `w`, read before the message box with no guard on it, so a sentence typed while the roster held the keyboard lost every `w` — `writing` arrived as `riting`. Widen is `alt+w` now. The bare letter works ONLY on the full-frame roster (under about 100 columns), where there is no message box on the screen; the hints say `alt+w wide`, `alt+w widen · click seam` and `alt+w narrow · click seam`."
  - "The rule that a bare letter stands down over a non-empty message box was written by hand in four files (stop.go's `x`, tasksettle.go's letters, harnesscard.go's `e`, task.go's `y r n`). It is `chordfocus.go`'s `app.chordsStandDown` and those four call it; that file also states which bare letters may exist at all — a key on a modal page with no box, or an answer to a question drawn on screen. A view toggle is neither, which is why `w` moved rather than gaining a guard."
  - "The settle answers were reachable only from a selected card in the transcript or from inside the node's room. With the roster holding the keyboard and the cursor on a row that needs your look, the hint slot now reads `a accept · l look again · n not right · esc` in place of the move keys, and those letters answer that row's landing from the column without opening the room."
  - "A task's check was pointed at `git diff --cached` and given nothing else about the tree, so a change whose important files were untracked was judged against the tracked half of itself. The packet now carries `THE GROUND, AS GIT SEES IT (git status --porcelain --untracked-files=all)` — staged, unstaged and untracked — read from the ground the checker stands in, capped at 100 paths, with `.aforge-v3` left out and nothing at all drawn for a workspace that is not a repository."
---

Four papercuts from supervising the 2026-08-31 dogfood run from the keyboard,
and they share one shape: the surface knew something the person needed and did
not say it. It knew where refused words could go, it knew a sentence was being
typed, it knew which three letters answer a landing, and the checker's own
packet knew which tree it was standing in.

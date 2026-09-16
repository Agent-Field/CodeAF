---
kind: changed
title: ctrl+c leaves on the press that lands
pr: 1061
surface: [chat, docs]
invalidates:
  - "The chat's door was TWO presses of `ctrl+c` inside 1.5 seconds: the first armed it, the hint slot read `ctrl+c again to quit` with what leaving would stop, and only the second one left. It is ONE press now. At rest `ctrl+c` writes the draft and exits on the keystroke that lands, over every picker, panel, room, page and paste bracket. Mid-turn nothing changed — it is still the interrupt, and that press is spent on the model — so a two-tap mid-turn now stops the answer and then quits."
  - "`quitarm.go` is gone; what remains of it — `leavingDraft`, the draft and every parked message folded into one string on the way out — is `internal/tui3/leaving.go`. `app.quitArm`, `armQuit`, `quitArmed`, `disarmQuit`, `quitSweep`, `quitHint` and `quitArmWindow` no longer exist, and neither does the hint slot's armed-door rung (render.go's `hintWord`). The work-counting half moved to `tabclose.go` as `workCount` / `workCountWord`, which is where it is still read."
  - "The line every session opens with was `esc interrupts · ctrl+c twice quits · ? for help`. It is `esc interrupts · ctrl+c quits · ? for help` — the same constant, `landingKeysWord`, and the e2e word table follows it. The `/help` sheet's row was `ctrl+c         twice quits · mid-turn one press interrupts, like esc` and is now `ctrl+c         quits everything · mid-turn it interrupts instead, like esc`."
  - "The warning that named what leaving would stop — `ctrl+c again to quit · 3 conversations · 2 tasks and a job will stop` — is not written anywhere any more. Nothing on the way out of the program names running work. The close-a-tab card (`ctrl+w`) still does, and it is the only card that asks before ending anything."
---

The owner asked for the key to mean what it means in every other terminal
program. The arm was protecting four real things, and three of them did not
need a keystroke to protect them: the draft and everything parked above it are
written by `app.quit` itself, which a real SIGINT and a closed window already
took; work a session host is running outlives the window whatever this surface
does; and the gap at the end of a turn loses nothing now that quitting in it
still folds the parked messages into the draft file
(`TestAQuitInTheGapAtTheEndOfATurnKeepsWhatWasParked`).

The fourth is genuinely spent. A person who leaves with tasks running inside
this terminal is no longer told so first, and `ctrl+c` struck by accident at
rest ends the session. What they typed comes back at the next launch; what those
tasks were doing does not. The manual says so in the asker's own words rather
than describing a door that no longer exists.

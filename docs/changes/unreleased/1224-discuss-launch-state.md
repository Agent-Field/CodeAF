---
kind: added
title: discussion and folder preview show launch state; pause coordination and stop work are two verbs
pr: 1224
surface: [chat]
invalidates:
  - "Wave 4 launch state had no TUI seam. Options.Exec is now the software-derived LaunchState / PauseCoordination / StopWork door; Collab does not grow launch verbs."
  - "Pause coordination and stop work were one idea on the surface. They are two verbs with two chords (`p` pause coordination, `s` stop work); tab-close `stop work` is the stop action, and closing a view pauses nothing."
  - "A missing executor would have looked like empty work. Nil Exec is no chrome; a present seam that cannot read names `launch state is not available` and never draws `0 runs`."
---

Natural-language launch still works when session.Config.Exec is wired.
No new slash command. `/folders` is unchanged.

---
kind: added
title: discussion and folder preview show launch state; pause coordination and stop work are two verbs
pr: 1224
surface: [chat]
invalidates:
  - "Wave 4 launch state had no TUI seam. Options.Exec is now the software-derived LaunchState / PauseCoordination / StopWork door; Collab does not grow launch verbs."
  - "Pause coordination and stop work were one idea on the surface. They are two verbs with two chords (`p` pause coordination, `s` stop work); tab-close `stop work` is the stop action, and closing a view pauses nothing."
  - "A missing executor would have looked like empty work. Nil Exec is no chrome; a present seam that cannot read names `launch state is not available` and never draws `0 runs`."
  - "cmd/codeaf opened collections.db for folders and left Config.Exec, Options.Exec, and RegisterExecutor unset, so launch-or-join was absent even after the Wave 4 packages merged."
  - "wsapi SetExecutor was never called on the production Open path, so IssueGrant persisted and LaunchOrJoin stayed a labelled absence."
---

Production Open binds wsexec.Adapter on collections.db, SetExecutor, RegisterExecutor, Config.Exec, and Options.Exec. Recover-by-key runs on the standing pass. A missing store still leaves the verbs absent.
No new slash command. `/folders` is unchanged.

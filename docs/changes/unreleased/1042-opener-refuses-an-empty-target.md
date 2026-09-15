---
kind: fixed
title: An empty target is nothing to open, and never a window on the working directory
pr: 1042
surface: [chat]
invalidates:
  - "`startOpener(\"\")` was believed to fail harmlessly. It does not. `open \"\"` on a Mac is not an error — the platform resolves the empty path to the CALLING PROCESS'S OWN WORKING DIRECTORY and puts a Finder window on screen. So the empty case was not a refused handoff with the link left standing; it was a file manager opening on somebody's folder with nothing on the surface to explain it. `startOpener` now refuses an empty or blank target before `exec.Command` is reached, with the sentence a PATH miss already says."
  - "`internal/tui3`'s own test suite opened a Finder window on ITS OWN SOURCE DIRECTORY on every run, in every worktree. `salience_test.go`'s table drives every event kind through the conversation's pump; `EventConnectAuth`'s row carries no `AuthURL`, its door is the browser handoff (connect.go), and the file put no stub over `processOpener`. If you remember `go test ./internal/tui3` as not touching the machine running it, that was only true of every OTHER door. `salienceRun` now takes `watchOpener`."
---

The four `internal/tui3` entries in the owner's Finder recents — one per checkout
that had ever run the suite, and nothing else from any of those trees — are what
made this findable. `open` raises an existing window rather than making a second
one, so a fault that fired once per test run read as ONE window shoving itself
in front of a person repeatedly, which is a much harder thing to connect to a
command you just typed.

The guard is in `startOpener` rather than at the sign-in call site on purpose.
Six doors reach the platform through that function, and the thing being refused
is a property of the target and not of any one door's story about it.

`opener_test.go` asserts the refusal WITH A WORKING OPENER ON PATH. A machine
with no opener refuses for a different reason and would pass the test with the
guard deleted, which is the shape the older
`TestNoOpenerOnPathIsAnAnswerAndNotAFork` already covers from the other side.

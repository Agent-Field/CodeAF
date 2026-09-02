---
kind: removed
title: the proposal hold of #279 is reverted from dev until it re-lands with askTask under the ceiling
pr: 367
surface: [engine, chat, remote]
invalidates:
  - "#279 landed on `dev` (2183719a) and its whole effect is on the branch: `HoldTask`, `taskQuestion`, `Task.Hold` on the wire at protocol ten, typing into the box holding the countdown, a bare no declining, and the fifteen-second default. It is reverted whole by this pull request. `dev` at this commit is back to #279's parent for all of that: the countdown is five seconds again, protocol nine, and typing does not hold the card. It re-lands in the next pull request with the same behaviour and `Agent.askTask` split along its phases."
  - "`dev` was red on `TestNoRoadInTheTaskEngineHasMoreEndingsThanItsLedgerRow` (`Agent.askTask (task.go) holds 18 decisions and the ceiling is 15`) from 2183719a to the parent of this commit. Bisected across the only two commits that touched `internal/session/task.go` since 779ed018: #279 alone crossed the ceiling and #333 left the count where it found it. The standing order is that `dev` is never red, so the offending commit is yanked first and re-landed fixed, rather than fixed in place."
---

The gate this tripped is the ratchet on how many endings one road may have, fitted in
7720d9b6 the evening before #279 merged. The PR gate did not run it — it runs only the
`Manual` tests of `internal/session` — which is why two pull requests passed with it red.
That is its own issue and its own fix.

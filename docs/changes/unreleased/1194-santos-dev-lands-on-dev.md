---
kind: fixed
title: santos/dev lands on dev, and a finished background job is reported done at once again
pr: 1194
surface: [engine, build]
invalidates:
  - "On Linux a background job's ending was reported two seconds late since #1155 (on `santos/dev` only, never on `dev`): the detached sweep runs while the job's shell is still a zombie, and `Group.Alive` probed the group with `kill(-pgid, 0)`, which a zombie answers, so every finished job read as still alive, was sent SIGTERM, and waited out the whole termination grace. `Alive` reads the group's members from /proc now and counts no zombie; a finished job is reported the moment its shell is reaped."
  - "The `touched packages` job on #1108 was red on every completed run of 2026-09-18 for two tests that pass on every laptop: `TestWirePoolIndexStartsTheRefreshAndThePush` counted one start-up errand because the pool resolver reads GitHub's `CI=true` as read-only (the test now empties `CI` for its duration), and `TestTurnBoundaryReportsRunningAndOneTerminalTransition` overran its four-second bound by the two seconds above. Neither is red on this head."
---

The 152 commits of `santos/dev` (#1108) are on `dev` as one squash; #1108 and
the ninety-three entries beside this one say what they carry.

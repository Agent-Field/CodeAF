---
kind: fixed
title: a checking call the window refused is no longer written down as a call that was made
pr: 812
surface: [engine]
invalidates:
  - "A landing nobody could check said `nobody could check it — asked twice, and neither call answered — nobody could check it in 40ms`: one sentence, the same question in it twice, and a window figure belonging to a call nobody made. That was a race, not a wording — the window is read once to decide whether a fresh checker is worth building and once more with it built, and on a loaded box the building spent what was left. The retry the second reading refuses is now said as what it is: the first call's own account, with `the window closed before a second` as a clause on the end of it."
  - "`internal/session`'s checking window is no longer read straight off `time.Now()`. It goes through `Agent.auditNow`, and `Config.auditClock` moves it for a test — the seam that makes the gap between those two readings reproducible without waiting for a loaded box to open it."
---

The report a person reads when nobody could check the work has to state ONE cause.
`auditVerdict.twice` says two calls were made and answered nothing; it was being
put over the sentence a refused call leaves behind, which already carries the
same subject. `Agent.auditOnce` now tells its caller whether a call was made at
all, and a retry the window refused lands exactly where the branch one rung up
already landed it.

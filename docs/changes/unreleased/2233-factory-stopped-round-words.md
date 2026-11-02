---
kind: fixed
title: Stopped and interrupted factory rounds keep their own outcome
pr: 2233
surface: [engine]
invalidates:
  - "Carrying on after Stop or a lost runner marked the earlier round as rejected, making chat say it didn't pass and was fixing failures."
---

After Carry on or retry, a round you stopped says `stopped`; a round cut by
a lost runner or engine restart says `interrupted`. Neither is rejected. A
real step error stays rejected and says `failed`. Reported completion rules,
round limits, spending and automatic retry limits are unchanged. Existing
saved round lines are not migrated.

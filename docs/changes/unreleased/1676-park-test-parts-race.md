---
kind: fixed
title: the park test waits for both parts to start instead of counting them at the park
pr: 1676
surface: [engine]
invalidates: []
---

TestAParentHandedADivisionBeforeItStartedOpensOnTheReportsAndNotOnTheWait counted
the parts its runner had seen at the moment the parent parked. A part reaches that
runner on a goroutine of its own, and the parent parks on the parts it has
outstanding rather than on their runners having started, so on a loaded CI box the
park came first and the test read one part. It now waits, bounded, for both.

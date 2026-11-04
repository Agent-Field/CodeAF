---
kind: internal
title: Client tools name their answer cutoff so the leaf-room law reads it as a wait
pr: 2360
surface: [app]
invalidates: []
---

The pending client-tool answer timestamp is named `answerBy`, and its selected
millisecond limit is named `waitMS`. The client's effective time limit, emitted
`DeadlineMS`, wait timer and expiry checks are unchanged.

---
kind: internal
title: routing beat tests wait for their beat to end before their synctest bubble closes
pr: 2918
surface: [engine]
invalidates:
  - "The routing beat tests in internal/provider started lanes.Beat and returned without waiting for it, so a beat asleep in the lane store's lock backoff at the test's last line panicked the bubble (\"main bubble goroutine has exited but blocked goroutines remain\"); every one of them now cancels the beat and waits for it to return."
---
`TestSilentRouterCanRecoverOnTheNextRoutingCompletion` failed three CI runs in a row on `org/main` this way while passing
every laptop run: the completion the test makes and the beat's reload wake at the same fake instant and race for the
store's file lock, and `synctest.Wait` treats the loser's backoff sleep as idle.

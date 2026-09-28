---
kind: fixed
title: a connect offer shows its sign-in reason together with the waiting state
pr: 1502
surface: [engine]
invalidates:
  - "TestWaitingSentenceConnectSignIn was a known CI flake that went green on rerun. The cause was real: a reader could see a conversation waiting on the person with an empty reason. The reason is now stored before the waiting state is published, and the test no longer flakes."
---

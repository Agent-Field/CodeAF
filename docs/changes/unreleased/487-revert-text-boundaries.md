---
kind: internal
title: the arbitrary-text preservation of #483 is off dev again until its tui3 test is green
pr: 487
surface: [chat]
invalidates:
  - "#483 said arbitrary text survives the naming and send boundaries of the chat. It is not on dev: it was reverted whole because it left internal/tui3 red (TestAMissingTypedWindowsPathNamesOneFileAndNeverSubmits, the missing typed Windows path leaving the draft empty), bisected to 5e0fa6b9 alone under an isolated home. The tree is byte-identical to the commit before it. It re-lands under the author's own number with the test green; this entry goes with it when it does."
---

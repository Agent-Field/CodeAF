---
kind: fixed
title: Red checks distinguish old failures from new ones
pr: 660
surface: [chat, engine]
invalidates:
  - A check command that was red before and after work was treated as one unchanged failure. When both outputs name individual failures, aforge now compares those identities so an old failure cannot hide a new one.
  - An unchanged baseline failure was described as proof that the work did not need to answer for it. It is now labeled as baseline evidence only, while the checker still judges whether the requested behavior was delivered.
---

Non-test validations still count when they turn from green to red. When red output cannot
be parsed into stable failure identities on both sides, aforge leaves the comparison
uncertain instead of guessing that the failure changed.

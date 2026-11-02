---
pr: 2008
kind: fixed
title: The Follow PR step says when it is paused on the budget
surface: [engine]
invalidates:
  - "Was true: a Follow PR paused on the job budget kept its step note reading \"watching\". Is true: the step's note says the budget was reached, and says watching again once a raised budget releases it."
---

The follow-up's reason and its step's note now change together when maintenance
parks on the budget and when a raised budget releases it.

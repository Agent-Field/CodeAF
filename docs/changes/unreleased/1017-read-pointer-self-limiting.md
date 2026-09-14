---
kind: fixed
title: the already-read pointer is self-limiting, so a model that lost the bytes gets them again
pr: 1017
surface: [chat, engine]
invalidates:
  - "A covered read was answered with the `[already read]` pointer as many times as the model asked, and a model that could not act on bytes already in its transcript re-read in circles until the loop guard stopped the turn. The claim now falls through to a fresh fetch after two pointer answers for one file, and the fresh record resets the counter."
---

Measured with deepseek/deepseek-v4-flash on a real ask: three pointer answers,
no work done, turn ended on the loop guard. The dedup still pays exactly where
the model uses what it holds.

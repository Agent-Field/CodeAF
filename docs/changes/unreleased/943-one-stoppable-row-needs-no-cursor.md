---
kind: changed
title: With exactly one stoppable row on screen, x raises its card without alt+t first
pr: 943
surface: [chat]
invalidates:
  - "`x` over the task roster needed `alt+t` first, always. It no longer does when exactly ONE stoppable row is visible: the key raises that row's stop card directly, and the row's own line names the key so the affordance is not something you have to already know. Two or more stoppable rows keep today's behaviour and `x` still types `x`, because with nothing aimed at, guessing which row to end is how an hour of work disappears."
  - "The argument for making the roster's keyboard askable — \"while the roster does not hold the keyboard there is no cursor and nothing is being aimed at\" — is still true and still the reason the multi-row case is unchanged. What it did not cover is the one-row case, where there is nothing to choose between, so the keystroke cannot land on the wrong thing. The law it serves, that one bare keystroke over a list must not be able to end an hour of work, is kept by the card, which asks and defaults to *keep going*."
---

The manual promised a one-key stop the surface did not give. The pages now say
exactly when `x` acts and when it types itself, which is the half a person needs
in order to trust either.

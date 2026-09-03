---
kind: fixed
title: aforge exec stops the round after three identical timed-out commands
pr: 510
surface: [engine, docs]
invalidates:
  - "A command that kept timing out with the same text re-ran indefinitely: each round the model re-issued it, the tool timed out again with the same error, and nothing stopped the cycle. The round now stops with `stuck-timeout` after MaxIdenticalTimeouts (3) identical timeouts — the command and count are named in the reason, and the threshold is read from the constant rather than written a second time."
---

Before, a round would keep re-running a command that timed out identically,
burning turns and budget with no bound. Now, when the same command times out
three times in one round, the round stops with `stuck-timeout`. The stop reason
names the command and the count, and the threshold is the constant
`MaxIdenticalTimeouts` — change it once and the guard, the reason and this
sentence all move together.
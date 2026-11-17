---
kind: fixed
title: An organization task Afi was running survives a server restart, and Start on a stuck Running task starts it
pr: 2747
surface: [factory, docs]
invalidates:
  - "A restart of the organization's server left every task Afi was running saying Running with nothing behind it, for good; now each is Interrupted and resumed once by itself within about ten seconds, and a second cut leaves it Interrupted with Start."
  - "Pressing Start on a task that said Running with nothing behind it answered 200 and did nothing (only Stop, then Start, recovered it); now it starts Afi on it again."
  - "A run that went quiet while the server was up stayed Running; after a minute without a beat it is Didn't report back."
---

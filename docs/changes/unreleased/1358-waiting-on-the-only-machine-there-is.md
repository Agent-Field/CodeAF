---
kind: changed
title: a conversation against one machine waits for it instead of giving up on it
pr: 1358
surface: [engine, chat]
invalidates:
  - "A quiet reply from a build with no router behind it spent a fixed allowance and then ended the turn — two attempts before #1343, four after it. A watched conversation with no model chain now has no allowance at all: it keeps asking, on a wait that climbs to ten seconds and holds there, until the machine answers or the person stops it. The deadline does not end it either, which is the only place in the failure policy where running out of time is not the last word."
  - "A verdict that asked for a wait after a cut did not get one. The turn loop read `!isCut` before applying a backoff, which was written when no cut had a backoff to ask for and stayed after #1343 gave one to a cut against a single machine — so that change bought four attempts and delivered them as fast as the endpoint could fail. The verdict's own figure is honoured now whatever shape produced it."
---
Every bound in the failure policy is there because the time could be spent on
something else. Another endpoint, when routing has a pool. Another model, when
a chain is configured. An ending, when the person can go and fix something with
what it tells them.

A person talking to their own server has none of those. Giving up returns them
to a prompt whose only sensible use is to ask the same question again, so the
ending is not a move — it is the harness making them do the retrying, worse,
by hand. And the cost of not ending is nothing: the machine is theirs, asking
it again is free, and what they are waiting for is usually weights finishing
their load or one busy slot coming free.

So `waitsForEver` is four facts, all load-bearing. A CUT, because a refusal is
an endpoint saying no and a cut is one saying nothing yet. ONE MACHINE, because
a pool has its own short allowance and its own reason for it. NO CHAIN, because
a person who configured a next model asked for the hop. And WATCHED, because
the same loop with nobody in front of it is a hang that spends a worker's whole
wall clock on a server that may never answer — a task node keeps its count.

THE SCHEDULE IS A RAMP TO A CEILING AND NOT A DOUBLING. Backing further and
further away is a manner towards a shared service under strain, and none of
that describes a machine on the same desk. It doubles while doubling is cheap
and then holds at ten seconds, because what is being waited for finishes at a
moment nobody can predict and everybody wants noticed at once: a schedule that
had reached four minutes between asks would turn a server that came back in
ninety seconds into four more minutes of watching a spinner.

AND THE WAITING IS SAID OUT LOUD, which is what makes the rest of it honest.
An unbounded retry is the one with no denominator to count towards, and a
status that reads `retrying` for ten minutes is indistinguishable from a wedged
program. The line carries the two facts a person would ask for and the one
thing they can do: `no answer 7 times in 2m · still asking · esc stops`.

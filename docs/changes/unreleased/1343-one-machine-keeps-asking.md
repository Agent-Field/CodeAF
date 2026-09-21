---
kind: fixed
title: a quiet reply from the only machine there is gets asked again, with a wait
pr: 1343
surface: [engine]
invalidates:
  - "A stream the guard cut that struck no endpoint spent two attempts with no wait between them, on the reasoning that the next ask lands in the same place by the same rules and buys nothing. That reasoning is about a POOL. A build with one machine behind it — a person's own base url, a local server, one connected service — struck nothing because there was nothing to strike, and the same narrow allowance ended the turn after two immediate asks."
---
`StreamCut.Rerouted` being false had two documented causes, routing switched
off and a stream that died before naming its server, and both want the short
allowance: the pool is still there, the next draw is the same draw. There is a
third cause and it wants the opposite answer. A request that named no machine
and was served by none has no pool at all, which is what a person running
against their own base url has on every request they ever make.

For them the harness asked twice, the same instant, and gave up. The wait it
withheld is the one thing that could have helped, because the move that makes
waiting pointless — being served by somebody else — does not exist when there
is nobody else.

So the cut now carries `OneMachine`, set where the adapter already knows it
(the request expressed no preference and no chunk named a server), and the
boundary answers it as its own shape: four attempts rather than two, and the
same doubling wait a refusal gets in front of each. Every other cut is
untouched — a rerouted silence still spends three with no wait, degeneration
still spends two — and a model chain, where one is configured, is still reached
the same way when the allowance runs out.

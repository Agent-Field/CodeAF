---
kind: fixed
title: A node retry asks the router to avoid the upstream machine that just failed it
pr: 1094
surface: [engine]
invalidates:
  - "A node call's three attempts used to be THE SAME REQUEST SENT AGAIN. `internal/exec`'s `complete` rebuilt each attempt from the caller's own context, so the router's default routing answered all three identically and a machine that failed the first failed all three — the run ended `after 3 node call attempts: provider ended the response with finish_reason=error` having produced no work. An attempt that failed on a named machine now records it and the next attempt carries it in `provider.ignore`, accumulating across the attempts of one call."
  - "The simple routing row used to mean NO `provider` OBJECT ON THE WIRE for a request with no pin. It still does for a first attempt; a retry after a failure that named its machine now sends one carrying the veto alone. A base that has not shown it carries a preference object, and a routing row of `off` — including the direct, one-road services that resolve to it — still send none."
  - "The field law over `only`, `order` and `ignore` used to live inside `Client.dropRefusedHere`. It is `providerPrefs.strike` now, walked by that method and by `Client.applyRetryAvoid` alike; behaviour is unchanged for the refusal list."
  - "The final sentence of a node call that failed every attempt used to name only the count and the last failure. It names the machines that were tried too, when any were learned: `after 3 node call attempts (providers tried: A and B): …`. A call whose failures named nobody reads exactly as it always did."
---

The list is the caller's own to scope: it is built in the node's retry loop,
handed over on each attempt's context, and gone when the call is over. Nothing
reaches a later call or a stored setting, and a call that never failed this way
carries an empty list — so every healthy request is byte-for-byte the request it
has always been.

A person's pin for the model wins outright and nothing is ignored beside it, on
the same law that forbids one object naming a machine and refusing it in the
same breath. A machine a demand still names is never also written into `ignore`,
and a demand the veto emptied stops being a demand rather than going out as
"these machines and no others" about no machines at all.

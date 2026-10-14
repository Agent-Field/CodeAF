---
kind: added
title: a leaked tool call is read back out of the model's own markup and dispatched, not thrown away
pr: 1835
surface: [engine]
invalidates:
  - >-
    A reply that was the model's own tool grammar written as text was always
    thrown away: the turn dropped, asked once more, the lane set aside, and on a
    single-model deployment the work ended there with "the reply was the model's
    own internal markup instead of an answer and was cut". When that markup held
    a well-formed call the endpoint had simply failed to parse, the call is now
    read back out and dispatched, and the turn carries on as the tool call it
    was always trying to be. A leak that cannot be read that way is still cut,
    and the refusals are the point: a tool the request never advertised, a
    parameter that does not parse, and a reply that is mostly words around its
    markup are all refused, because a model explaining a call must never have
    its explanation executed.
  - >-
    /settings' `reply guard` was the only way to see such a reply, and turning
    it off also turned off every guard beside it. It is still the same switch,
    and it now governs the recovery as well as the cut: off means the markup is
    never read back out, so the turn is cut as before.
---

Some endpoints serve a model without translating its private tool-calling
syntax, and asked to use a tool it writes the call as visible text. That reply
is now recovered rather than discarded, under three refusals that are each
load-bearing: the named tool must have been advertised, the arguments must
parse, and the markup must be the substance of the reply rather than a
quotation inside it. The last of those is a measured bound — captured leaks
measure 0.25 to 0.41 prose around their markup, replies quoting an invocation
measure 0.57 and up — and the measured numbers are asserted, so anyone moving
the bound has to move the evidence with it.

Tested against six replies this harness actually threw away in production.
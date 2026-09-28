---
kind: fixed
title: restore connected-provider refresh and preserve recall re-ask outcomes
pr: 1656
surface:
  - chat
invalidates:
  - "A cancelled generation could finish before recall recorded its re-ask; cancellation and that outcome now publish together."
  - "Ordinary linked-local launches only refreshed the default catalog; they now refresh connected providers and expose their refresh actions."
  - "A cached model list concealed the provider's latest refresh failure; the refusal now stays visible beside cached choices."
---

Ordinary local conversations now share provider catalog warming, refresh and
notifications with in-process conversations, using the local engine's profile.
Provider settings regain their refresh action without changing the active model.
Remote connections retain their existing profile boundaries.

A failed refresh leaves cached models usable and shows the refusal above that
provider's choices, including under model filtering and column sorting. A
successful retry clears the refusal. Closing the picker during a refresh keeps
it closed when the response arrives.

Recall now records that it re-asked a request before the cancelled generation can
finish, so a fast reply cannot omit that outcome from the turn record (#1655).
The late-recall regression explicitly orders recall after the first visible word.
Session fixtures also wait for asynchronous shutdown before checking process exit,
preserve the inherited Go build cache before isolating HOME, and settle background
readings before a finite revised-direction script answers. Existing real audit
commands and acceptance assertions are retained; no timeout was increased.

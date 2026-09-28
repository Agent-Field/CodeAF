---
kind: fixed
title: refresh connected providers on ordinary local launches and show cached-list failures
pr: 1656
surface:
  - chat
invalidates:
  - "ordinary linked-local launches only refreshed the default catalog and omitted custom-provider refresh actions"
  - "a cached model list concealed the provider's latest refresh failure"
---

Ordinary local conversations now share provider catalog warming, refresh and
notifications with in-process conversations, using the local engine's profile.
Provider settings regain their refresh action without changing the active model.
Remote connections retain their existing profile boundaries.

A failed refresh leaves cached models usable and shows the refusal above that
provider's choices, including under model filtering and column sorting. A
successful retry clears the refusal. Closing the picker during a refresh keeps
it closed when the response arrives.

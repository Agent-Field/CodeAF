---
kind: internal
title: a test's first event no longer hangs cmd/codeaf when it beats the backend's watch
pr: 2196
invalidates:
  - >-
    Was true: fakeConversation made its question and task lanes only when the
    backend started watching, after the agent was already live, so a test's
    first raise could send on a nil channel and hang the whole package until
    the timeout. Is true now: an event put before the watch is held and handed
    over in order when the lane opens.
---

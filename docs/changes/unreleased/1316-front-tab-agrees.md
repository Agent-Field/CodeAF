---
kind: fixed
title: the front tab and the tab beside it agree about whether one conversation needs a person
pr: 1316
surface: [chat]
invalidates:
  - "A conversation holding a standing answer or a landed task saying `your call` carried the needs-a-person mark while it was held and LOST it when a person brought it forward, because the front tab re-spelled the lane list on the surface and left those two lanes out. Both sides now count them."
---
frontSignal answers from the surface rather than from the engine, deliberately,
because Agent.NeedsPerson takes the agent's mutex and allocates a map and the
strip is laid out every frame. That is unchanged. What is added is
asksStandingOrLanding, one allocation-free walk over the questions the surface
already holds, in the same shape as the harness and consent readings beside it.
An automatic proposal carrying a deadline is still excluded on both sides,
because a countdown is not a question.

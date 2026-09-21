---
kind: fixed
title: the front tab and the tab beside it agree about whether one conversation needs a person
pr: 1316
surface: [chat]
invalidates:
  - "A conversation holding a standing answer carried the needs-a-person mark while it was held and LOST it when a person brought it forward, because the front tab re-spelled the lane list on the surface and left that lane out. Both sides now count it."
  - "A landed task saying `your call` is deliberately NOT counted by either tab's mark. Its question object stays open through the whole settle after the person has answered, so counting it would hold a mark up over an answer already given. It is drawn in the conversation itself and on its own card."
---
frontSignal answers from the surface rather than from the engine, deliberately,
because Agent.NeedsPerson takes the agent's mutex and allocates a map and the
strip is laid out every frame. That is unchanged. What is added is asksStanding,
one allocation-free walk over the questions the surface already holds, in the
same shape as the harness and consent readings beside it. An automatic proposal
carrying a deadline is still excluded, because a countdown answers itself.

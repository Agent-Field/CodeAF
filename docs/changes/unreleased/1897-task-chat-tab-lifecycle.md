---
kind: fixed
title: task tabs close without replacing or trapping their main chat
pr: 1897
surface: [chat, docs]
invalidates:
  - "A run tab used conversation dismissal and could reappear immediately or be chosen as its owner's next conversation. Task views now dismiss independently, stay hidden until explicitly reopened, and never enter conversation close navigation."
---

The task tab's close button and ctrl+w on its selected view return to the main
chat without stopping work. Closing the last main chat goes to Home even when
it has a live run; remembered task views cannot migrate into another chat's tabs.

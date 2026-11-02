---
kind: changed
title: codeaf app starts a suggested choice's clock only once a surface has drawn its card
pr: 2155
surface: [engine]
invalidates:
  - "Was true: the app's choice clock (proposed_task_clock_s, 12 s by default) started when a choice with a suggested answer was raised, so a question no surface had drawn, such as one from an agent the person never opened, was answered with the suggestion before anybody saw it, blocking or not. Is true now: such a choice is raised with no clock (no clock_until) and waits for the person; the clock starts, for its whole length, when a surface sends asks.shown for it, and that start is announced to every window with ask.opened. Hello says `asks.clock.drawn`. A project's own autonomy row for choices still runs from the raise."
---

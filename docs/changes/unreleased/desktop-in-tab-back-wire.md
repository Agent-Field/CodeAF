---
kind: fixed
title: Desktop task return follows window focus history
surface: desktop
invalidates:
  - "The task parent button and bracket keys always walked task-only route history. They now use the shell's focus-history controller when supplied, with local routing retained for standalone views."
---

The immediate-parent header retains Iteration 2 geometry. Click and Command+[
travel exactly one history step, even when the previous focus was another tab.
An already-handled keyboard event is not consumed again.

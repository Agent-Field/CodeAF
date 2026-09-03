---
kind: changed
title: Check-refused tasks now say incomplete and keep their reason
pr: 512
surface: [chat, docs]
invalidates:
  - A task whose check refused its claim was shown as failed with a cross; it is now shown as incomplete with a steer mark while the engine still stores failed plus refused.
  - Task history could split a visible child from its visible parent when their states put them in different sections; a visible family now stands together in its most urgent section.
---

A check that names unfinished work is a useful next step, not a runtime error.
The card and history keep that exact reason visible, while actual errors, old
rows with no ending, stops, and work nobody could judge retain their existing
states.

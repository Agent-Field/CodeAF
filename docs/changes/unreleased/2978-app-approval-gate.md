---
kind: fixed
title: Keep completed approval steps marked done in app runs
pr: 2978
surface: [app, engine]
invalidates:
  - A standalone approval could appear as planned after the run saved its result. Newly approved gates stay marked done as the run moves on.
---

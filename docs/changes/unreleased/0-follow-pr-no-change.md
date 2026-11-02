---
kind: fix
title: Follow PR considers comments without manufacturing changes
pr: 0
surface: factory
invalidates: []
---

Follow PR can finish a current-head review of a comment that requests no edit
without making a new diff. Missing review checklist fields now reach the model
when it reports, allowing correction in the same conversation instead of spending
another round to discover the omission. Merge still requires its existing gates.

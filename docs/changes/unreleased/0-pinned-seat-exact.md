---
kind: fixed
title: A pinned role stays on its model when another role on that model falls back
pr: 0
surface: [engine]
invalidates:
  - "A role that stayed on its model while another role sharing that model fell back was sent to the fallback on its next call. A saved pin, a pin for this task, and one model for every role stay on the model the task records for that role."
---

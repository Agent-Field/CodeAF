---
kind: fixed
title: Approved plans resume after their gate without running the plan again
pr: 1857
surface: [engine]
invalidates: []
---

Factory workers report a plan approval question against its approval gate, preserving the completed plan. Both a running worker and a worker resuming later continue after that gate once a person approves. The precise misfiled shape saved by older workers recovers through the normal approval flow without editing saved state or skipping another step.

A new approval question requires a fresh decision and five-second Undo window. An earlier approval receipt stays in Activity and cannot hide the next Approve action or consent to the next ask. Worker reports still cannot pass gates. Cold Send back remains a separate limitation.

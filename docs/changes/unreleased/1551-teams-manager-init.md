---
kind: fixed
title: manager conversation initialization, handle fallback, and approval posture inheritance
pr: 1551
surface: [chat, engine]
invalidates:
  - "Sub-team manager conversations started via team_start previously ran under default approval postures rather than inheriting the initiating conversation's posture. They now inherit and persist the initiating posture."
  - "Top-level team managers without an explicit title previously lacked a fallback handle. They now receive an assigned fallback handle upon creation so they can be messaged immediately."
---

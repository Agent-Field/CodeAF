---
kind: fixed
title: manager conversation initialization, handle fallback, and approval posture inheritance
pr: 1597
surface: [chat, engine]
invalidates:
  - "Sub-team manager conversations started via team_start previously ran under default approval postures rather than inheriting the initiating conversation's posture. They now inherit and persist the initiating posture."
  - "Top-level team managers without an explicit title previously lacked a fallback handle. They now receive an assigned fallback handle upon creation so they can be messaged immediately."
  - "Approval inheritance now uses the session-owned setter before team membership can wake work. A failed setter refuses startup; the UI no longer overwrites session metadata or borrows an unrelated foreground conversation’s posture."
---

---
kind: fixed
title: a team's wrap-up survives a restart, and a cap packet is raised once
pr: 1494
surface: [engine]
invalidates:
  - "A restart lost a running wrap-up's start and bound. They are now kept in teams.json and resumed, and a busy decisions file leaves the wrap-up due instead of dropping it."
  - "Two processes crossing a team cap each raised a packet. One packet is raised per crossing."
---

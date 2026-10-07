---
kind: fixed
title: the hosted drive passes on a Mac whose task column starts open
pr: 1756
surface: [build]
invalidates:
  - "On a Mac, scripts/hosted-drive.sh failed seven of its fourteen checks whenever the profile's task column started open, which reads as a broken binary. It now recognises the column's opt+l key and passes."
---

---
kind: fixed
title: A factory with no filesystem watch warns once and its pulls look once a second
pr: 2165
surface: [engine]
invalidates:
  - "Was true: when the factory could not open a filesystem watch, each open pull looked every 100 ms with no warning. Is true now: it logs one warning naming the cause and looks once a second."
---

---
pr: 2008
kind: changed
title: Ask-first paths and never paths are one kind of rule, protected paths
surface: [engine]
invalidates:
  - "Was true: a step's brief listed ask-first paths and never paths on two lines (\"ask before touching\" and \"never touch\"), and a match was held as \"ask first · touch auth/ · …\", \"never · …\" or \"never starts on its own · …\". Is true: both are protected paths, told to every step on one line (\"ask before touching: auth/, infra/\"), and any match at a start, a plan or a change is held as \"protected · <path> · <file>\"."
---

A never path held a job at exactly the same points as an ask-first path (its
start, its plan and its change) and differed only in what the steps were told.
Both are now protected paths with one brief line and one hold wording. Files
that still write `never:` keep working: those paths are read as protected
paths. Ask-first sentences keep their own line and their own words.

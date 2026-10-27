---
kind: fixed
title: Every start carries its start rules, and the manager cannot approve a plan a rule holds
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "Was true: only a person's Start of an existing job and an automatic start stamped the job with its start rules (the repository's and the factory's ask-first and never lines). A job created and started in one request (`POST /v1/jobs` with `autostart`, including a hand-off) and one the manager started with `factory_start` carried none, so the approval after its plan never checked them. Is true now: queueing a job stamps them when no earlier door did, read from the job's repository with the factory's rules; with the repository's file unreadable the job carries the factory's lines, is marked as started with its rules unread, and its plan waits for a person."
  - "Was true: `factory_start` took the manager past any approve step, including the approval after a plan that a rule holds for the person. Is true now: it refuses with \"#N waits on the person · a rule asks first: …. Only they can approve it, on the job\" and the job stays where it waits."
---

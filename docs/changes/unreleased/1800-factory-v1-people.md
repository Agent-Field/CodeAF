---
kind: added
title: Factory person cards show profiles, ownership and what waits on each person
pr: 1800
surface: [chat, remote, docs]
invalidates:
  - "Factory identity reads described only signed-in members. GET /v1/people/{handle} now includes public GitHub profiles for outside authors, visible ownership, assigned and waiting counts, and machine presence."
  - "Queue rows carried handles without shared profile data or file owners. GET /v1/queue now carries people summaries for every mentioned handle, and jobs carry owners from default-branch CODEOWNERS."
---

Contract stays 1. Profiles reuse a 24-hour cache and GitHub ETags. Ownership is
cached until the next poll, follows CODEOWNERS location precedence and last-match
rules, and never reads a job branch. Cards and queue maps expose only visible
repository facts; partial viewers see only machines holding visible jobs.

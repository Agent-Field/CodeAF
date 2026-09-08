---
kind: fixed
title: same-named tags cannot disguise protected landing branches
pr: 654
surface: [engine, chat]
invalidates:
  - "A tag named dev made Git's shortened branch spelling heads/dev, which bypassed the protected-branch list and let a task merge there. The landing policy now reads the actual name from the full refs/heads reference."
  - "The moved-tip guard resolved short names that tags could share and read signature-display output as committer identities. It now resolves branch commits and ancestry through exact refs/heads names and disables signature display for its committer reading."
---

Independent review after #626 merged found this edge case. A real task landing
with a same-named dev tag returned merged before the correction; the regression
now requires branch kept and an unchanged protected checkout tip. Existing
moved-tip, restoration and simultaneous-session landing controls remain green.

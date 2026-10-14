---
kind: fixed
title: review agents reserve answer time and retain failure evidence
pr: 1817
surface: [chat, engine, docs]
invalidates:
  - "Review sessions were forced to answer only at their turn or context limits. They now reserve answer time inside the existing hard deadline."
  - "Session duration was lost when the result returned. Completed and failed sessions now retain elapsed time and failed-agent diagnostics."
  - "A pipeline error returned without saving a review record. It now saves separate non-postable failure records; the failed pipeline ending is preserved."
---

Failure records contain no assembled review and cannot be posted as GitHub reviews. Time and cost ceilings are unchanged.

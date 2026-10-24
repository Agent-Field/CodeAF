---
kind: fixed
title: automatic task starts require a clear request to act now
pr: 1845
surface: [chat, engine]
invalidates:
  - >-
    Questions and product direction could be mistaken for permission to start a
    task. Both route judges now distinguish clear polite requests from
    feasibility questions, ambiguous questions and future direction. Only a
    wake exclusively reporting known stopped results for the current request,
    without fresh human input, skips automatic task routing.
---

Clear polite requests can start work even when phrased as questions. Questions
about feasibility, ambiguous questions and future direction stay in the
conversation. The stopped-result guard applies only to an exclusively result-driven
wake where every result is known to be stopped for the current request and no fresh
human message was consumed. It makes no classifier calls and admits nothing.

Every other case keeps normal routing inputs, limits and rulings: fresh human messages
(including acknowledgements during a wake), mixed results or attempts, different
requests, background news and missing provenance. Mixed wakes have no new guarantee
that stopped work will stay excluded. Restoring a saved task preserves its stopped
flag; the guard still requires provenance on the delivered result. Ordinary recovery
notices without that provenance follow normal routing.

The authorization and stopped-result rules each have their own searchable manual
section, so retrieval returns the rule instead of truncating it inside a longer topic.

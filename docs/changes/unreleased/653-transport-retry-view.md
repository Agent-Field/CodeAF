---
kind: fixed
title: Replace failed network attempts on the live conversation page
pr: 653
surface: [chat]
invalidates:
  - "A retryable network error could leave its partial answer above the replacement on the live page, while the saved conversation contained only the replacement. The same discard event now resets both views before an actual retry."
  - "Stopping during retry backoff still keeps the partial reply; an exhausted attempt does not announce a replacement that will never start."
---

The existing retry phase continues to describe the wait. Once the next request
can start, the page says `the request failed — asking again` and removes the
failed attempt's streamed content. Request budgets and backoff are unchanged.

---
kind: fixed
title: senior-dev's web search no longer puts the Exa key in its URL or its errors
pr: 2223
surface: [engine]
invalidates:
  - >-
    Was true: senior-dev sent EXA_API_KEY as a query parameter, and a failed search
    printed that URL with the key in its error. Is true now: the key goes in an
    Authorization header and no error holds it. A key from an earlier failed search
    can still be in a saved session or log, so replace a key that may have been exposed.
---

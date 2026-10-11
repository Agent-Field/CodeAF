---
kind: added
title: Factory members, sessions and per-viewer repository visibility
pr: 1800
surface: [chat, remote, docs]
invalidates:
  - "Member lists, session management and per-repository visibility were outside factory sign-in. GET /v1/members, GET /v1/me/sessions, DELETE one session and POST /v1/me/sessions/signout-others are now on /v1."
  - "Every signed-in caller saw the whole floor. A GitHub viewer now sees only repositories their token can read, cached for 10 minutes, and a viewer who can see none gets an empty queue with reason no_visible_repos."
  - "Device sign-in requested only read:user, and the GitHub token was dropped after the profile check. Sign-in now also requests repo, and that token stays in process memory for the visibility check; it is still never returned or written to disk."
---

Contract stays 1. Existing /v1 fields are unchanged. Loopback and the host
access token still see the whole floor.

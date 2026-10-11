---
kind: added
title: Factory GitHub device sign-in identifies each caller
pr: 1800
surface: [chat, remote, docs]
invalidates:
  - "Every remote caller previously inherited the factory owner's GitHub login. Device-flow sessions now carry the caller's GitHub profile, and /v1/me, queue.me and HTTP action audit handles resolve that caller."
  - "The host config previously had no GitHub sign-in settings. github_client_id enables device flow and optional team restricts signed-in GitHub handles; loopback remains open as the owner."
---

Add POST /v1/auth/device and /v1/auth/device/poll behind the existing access gate.
Successful sign-in checks GitHub's user, issues a browser cookie and factory
bearer credential, and exposes name, avatar_url, session_id and scopes alongside
all existing /v1/me fields. Contract stays 1. Sessions live in memory for 30 days;
GitHub tokens are neither returned nor persisted. Member lists, session management
and per-repository visibility are outside this lane.

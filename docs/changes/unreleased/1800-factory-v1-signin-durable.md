---
kind: fixed
title: Factory GitHub sessions survive restart and refresh expiring tokens
pr: 1800
surface: [chat, remote, docs]
invalidates:
  - "Factory sign-ins were process memory and every restart signed people out. Sessions now survive restart with hashed factory credentials and encrypted GitHub access and refresh tokens in private files beside tokens.json."
  - "An expired GitHub token silently hid repositories for the rest of a factory session. Expiring tokens now refresh before expiry; GitHub 401 or bad_refresh_token ends the session with HTTP 401 reason github_expired, including after restart."
  - "The queue omitted watched repositories before their first job and could say no_visible_repos to their owner. It now includes visible watched repositories with zero jobs."
---

Contract stays 1. Document OAuth App creation, enabling Device Flow and
setting github_client_id. Refresh outages keep the session and answer 502.

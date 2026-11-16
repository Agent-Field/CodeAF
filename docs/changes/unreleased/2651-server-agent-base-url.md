---
kind: fixed
title: An organization server's workstream agent answers without CODEAF_BASE_URL set
pr: 2651
surface: [factory]
invalidates:
  - "An organization server must be started with CODEAF_BASE_URL for its workstream agent to answer; without it every turn fails with \"provider base URL is required\"."
---

The workstream agent on an organization server sends its turns to OpenRouter (`config.DefaultBaseURL`) when the server's process has no `CODEAF_BASE_URL`; a set variable is still the operator's own proxy, and the server profile's `base_url` is still never read. Before this, an unset variable meant an empty base URL, which the provider client refuses, so every server turn failed on a server started without it.

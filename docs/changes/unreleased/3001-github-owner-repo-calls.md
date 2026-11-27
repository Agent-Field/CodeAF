---
kind: fixed
title: GitHub connector calls that take an owner and a repo work again
pr: 3001
surface: [chat, engine]
invalidates:
  - "Every GitHub tool call that names an `owner` and a `repo` failed, even for a public repository. GitHub's tool server marks both arguments to be sent as HTTP headers, from protocol version 2026-07-28 it refuses a call that lacks them, and each call opened a session that never listed the tools, so the headers were never built. The call now lists the server's tools in its own session first and the headers go out."
---

One extra `tools/list` round trip per call. A server that refuses to list is
still called, on a fresh session.

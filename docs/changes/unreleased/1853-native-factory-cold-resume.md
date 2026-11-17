---
kind: fixed
title: Restored native conversations no longer take over the local factory
pr: 1853
surface: [engine]
invalidates: []
---

Opening a conversation in the native app no longer starts the terminal factory worker, poll or triage. A saved owned local factory resumes quietly on its current address, and only explicit Run starts the app runner. Terminal startup and refusal to adopt another process’s service are unchanged.

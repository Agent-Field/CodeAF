---
kind: added
title: CODEAF_GITHUB_LOG records each GitHub request by endpoint and cause
pr: 2216
surface: [engine, docs]
---

Set `CODEAF_GITHUB_LOG` to a file to get one JSON line per GitHub request: the
factory route or loop that caused it, the endpoint with every name taken out,
the status and the rate limit GitHub reported. It is off by default and holds no
token, query, body, repository name or file path.

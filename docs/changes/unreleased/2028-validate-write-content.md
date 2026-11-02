---
kind: fixed
title: Reject write calls missing string content before changing files
pr: 2028
surface: [chat, engine]
invalidates:
  - "A missing or null write payload could silently create or truncate an empty file; compact, chat and append writes now require their string payload field."
---

Compact workers require `text`; chat writes require `content`, including appends.
Malformed calls return a tool error before creating directories or changing files.
An explicitly supplied empty string remains a valid request to write an empty file.

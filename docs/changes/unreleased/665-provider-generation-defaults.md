---
kind: fixed
title: SDK tool calls preserve provider defaults and explicit generation choices
pr: 665
surface: [engine]
invalidates:
  - "The SDK fallback used to inject a configured output cap and erase explicit temperature choices. It now clears SDK defaults before applying caller options."
---

Default requests omit temperature and output limits at this boundary; explicit
caller options, including zero temperature, survive. This does not change the
request deadlines or the tool and turn limits.

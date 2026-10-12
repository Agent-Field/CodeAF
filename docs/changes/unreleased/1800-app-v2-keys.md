---
kind: added
title: App keys arrive through memory and inherited pipes without disk writes
pr: 1800
surface: [engine, remote, build, docs]
invalidates:
  - "A1 discarded stdin key handoff and served no key routes. The app now ingests the first line into memory and serves authenticated GET, PUT and DELETE key routes with one bounded provider check."
  - "API.md assumed a separate app engine home. The owner's decision makes app and terminal share CODEAF_HOME or ~/.codeaf; app key writes are refused while ordinary settings and terminal credentials remain shared."
  - "A child engine could resolve its key only from shell or profile. App child launches now hand over memory credentials on an inherited pipe, with keys absent from arguments and environment."
---

Memory removals suppress shell/profile fallback, and failed PUT checks retain
working credentials. Provider and factory-session values register with exact
redaction before any result can be recorded. A2 starts no factory worker;
the shared-home worker election remains in the factory startup code.

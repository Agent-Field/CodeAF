---
kind: added
title: missing OpenRouter profiles connect in the browser
pr: 206
surface: [chat]
invalidates:
  - "A local chat with no API key eventually failed at its first model message or required finding the OpenRouter row in settings. It now offers a one-press browser connection before any model call."
  - "The first-run provider step was suppressed forever after it was skipped or its key disappeared. The OpenRouter prerequisite now returns for eligible new, existing, and resumed conversations while the default provider has no key."
  - "Interactive named and resumed sessions refused to open with no API key. On the built-in OpenRouter endpoint they now open the S256 PKCE connection; headless, hosted, and custom endpoints still require an explicit key."
---

The returned key lands in the existing owner-readable profile field and reaches
every conversation already open in the process. Pasting an existing key remains
available on the same screen.

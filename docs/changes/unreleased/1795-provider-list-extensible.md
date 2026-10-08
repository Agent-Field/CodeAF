---
kind: fixed
title: Built-in provider lists accept appended catalog rows
pr: 1795
surface: [chat, docs]
invalidates:
  - "The provider chooser kept its key hint even when it pushed OpenRouter off a short terminal. The hint now yields before any provider row, so a tenth entry fits at 40x10."
  - "Provider-menu tests pinned the catalog size or assumed Custom API was last. Counts now follow the catalog, appended sources preserve the established order, and custom-row tests select by identity."
  - "The manual repeated numeric built-in provider counts and complete provider lists. Those passages and the guide now describe every built-in provider without a fixed count, and a manual guard rejects count claims."
---

The built-in catalog is unchanged. A throwaway source appended to Vendored()
passes the full tui3 and manual suites, and the short chooser shows every row
without its footer. The separate provider addition can land without changing
these setup tests or manual passages.

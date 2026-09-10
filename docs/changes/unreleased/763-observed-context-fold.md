---
kind: changed
title: long turns reduce old observed results without requiring a file change
pr: 763
surface: [engine, chat]
invalidates:
  - "Current-turn folding required a later successful file change and admitted only read, grep, find and ls. Any completed tool batch that the model has already seen can now receive recoverable head-and-tail views once it leaves the protected recent window."
  - "A protected remainder could prevent all current-turn savings unless eligible results reached the entire lower target. Useful partial reductions now proceed when actual savings meet the existing cache-reclaim share; tiny cache-breaking rewrites still do nothing."
  - "Current-turn folds replaced results with one-line stubs. They retain the existing reduced view's head, tail, elision count and retrievable original, while preserving non-text content and leaving already-reduced views unchanged."
---

The observation horizon and complete-batch boundaries replace file-write
consumption bookkeeping. Original requests, assistant text and call arguments,
unseen results and recent working context remain intact. This does not change
speculative tool execution, generation settings, completion rules or budgets.

A result's omitted middle remains recoverable rather than always visible in the
prompt. Regression tests establish those retention and replay properties, not
an improvement in benchmark quality, time or cost.

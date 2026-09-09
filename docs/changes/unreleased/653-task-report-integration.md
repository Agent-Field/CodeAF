---
kind: fixed
title: Preserve code blocks in task reports without repeating the answer
pr: 653
surface: [chat]
invalidates:
  - "Task execution now uses the bounded report formatter, keeping its short fenced tail instead of cutting a code block after the first three lines."
  - "Expanded task cards recognize both current report previews and older three-line previews, retaining delivery diagnostics while showing the full answer once."
---

The worker's full answer and overflow file remain available independently of the
compact report. Existing preview bounds and result retention limits are unchanged.

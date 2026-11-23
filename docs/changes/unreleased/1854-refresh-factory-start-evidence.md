---
kind: fixed
title: Issue edits clear stale factory start evidence
pr: 1854
surface: [engine]
invalidates: []
---

Changing an issue’s title or body clears the model’s earlier start judgement before the new text is read. The issue’s body-length size estimate is refreshed; explicit size and priority labels remain intact.

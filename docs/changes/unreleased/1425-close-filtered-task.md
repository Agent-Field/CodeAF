---
kind: fixed
title: Closing a task removes it from the current Sessions results immediately
pr: 1425
surface: [chat, docs]
invalidates:
  - Closing a task while filtering Sessions left it visible because searches include archived tasks; it now disappears from the current results, and changing the filter makes it discoverable again.
---

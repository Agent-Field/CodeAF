---
kind: fixed
title: codeaf app's memory list says where to add a model key
pr: 0
surface: [engine]
invalidates:
  - "Was true: with no key at launch the memory list read `saved memory can't be read: OPENROUTER_API_KEY is required`."
---

With no model key, the memory list now reads "saved memory can't be read without a model key: add one in Settings › Accounts › Providers".

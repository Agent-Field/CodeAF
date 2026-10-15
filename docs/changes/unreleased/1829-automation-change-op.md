---
kind: added
title: the conversation can change an automation in place, on a save this change? card
pr: 1829
surface: [chat, engine, docs]
invalidates:
  - "The `automation` tool had no way to change an existing automation, so \"move my reminder to 5pm\" proposed a second one beside the first, often a finished one. It has a `change` op: an id and only what changes, saved through `Store.Update`, which wakes a finished one-time reminder given a new moment."
  - "The manual said the chat had no verb that changes an automation in place, and that changing one was `e` or a new proposal that replaces it. Asking in the conversation changes it in place, and `e` on `/automations` still does."
  - "`save this change?` was a card only a typed `/automations edit` raised. The model's `change` raises the same card; any card whose automation already has an id is a change, with only `1 Save` and `0 Don't save`."
  - "The prefix waivers were 4,713 bytes on the fixed arm and 13,435 on the lean. They are 4,691 and 13,413: `change` was paid for out of the tool description's repeat of the watch fields."
---

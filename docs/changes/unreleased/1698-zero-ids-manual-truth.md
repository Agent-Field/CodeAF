---
kind: fixed
title: zero counts draw nothing, no raw ids as names, and the manual matches the screen
pr: 1698
surface: [chat, docs]
invalidates:
  - "The task column header drew `Tasks 0` (and a manager's `Tasks 0 · Traffic 0`), the status line drew `0 tok/s` for a rate below 1, and the thinking label drew `thinking · 0 tok`. A zero or unknown count now draws nothing; the word stays."
  - "The sessions place named a conversation it could not find `Conversation <16-hex id>`. It now reads `new conversation`, the unnamed word Home uses."
  - "The manual said the /model picker always groups models under provider headings; with only the default provider registered there is no heading (adding Ollama draws them even without the default provider's key)."
  - "The manual said /new, and opening another project, make the tab strip show both conversations. An untouched new conversation has no tab until it has a draft or a first message."
  - "TestTUIE2E/space_in_the_task_room_pages_the_card required the window's own untitled conversation on the first 14-row screen and failed whenever the earlier task landed `your call`. The driver now walks to it."
---

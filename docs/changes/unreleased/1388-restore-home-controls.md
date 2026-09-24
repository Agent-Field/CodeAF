---
kind: changed
title: Home returns to double-space navigation and the ask-here choice
pr: 1388
surface: [chat, docs]
invalidates:
  - "Escape navigated outward until Home and two spaces only typed text. Two spaces in an empty box open Home again; Escape closes places into the conversation, interrupts its running answer, and double-Escape opens inline rewind."
  - "Home showed only search results above the seam and /ask selected a Home exchange. The ask-here and start-a-new-conversation rows are back below the results; Enter defaults to a conversation, Up then Enter asks here, and /ask is removed."
---

Restore the two interactions changed in #1071 while keeping subsequent command-menu,
effort-shortcut, question-row, compact-layout and waiting-message fixes. The manual
and terminal test recipes follow the restored keys. Deterministic tmux coverage drives
the built binary at three widths and checks both submission routes with a local endpoint.

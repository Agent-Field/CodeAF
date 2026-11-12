---
kind: added
title: desktop can read and change conversation skills
pr: 0
surface: [chat, engine]
invalidates:
  - "Was true: the desktop had no typed method for the terminal's skill shelf or attachments. Is true now: skills.list reads the conversation shelf with attached names first, and skills.set attaches or detaches using the terminal's own doors. Both methods are advertised in hello; successful sets emit skills.changed. The terminal and wire share shelf merging and ordering, and attached rows retain their description and source. Hosted shelves and attachment changes use the session host."
---

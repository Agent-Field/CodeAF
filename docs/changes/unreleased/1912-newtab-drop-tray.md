---
kind: fixed
title: Dropped files show as removable chips on the new-tab start page
pr: 1912
surface: [chat]
invalidates:
  - "A file dropped on the new-tab start page used to attach invisibly and surface only once the chat opened. The start page now draws a removable attachment chip the moment the drop lands."
---

Dragging a file onto the start page (before any chat exists in the tab) landed
the attachment in the pending composer but drew nothing. The welcome unit now
renders the same removable chip row the conversation's composer uses, above the
draft, so a drop is visible immediately and can be undone with backspace on the
empty box or a click on the chip before the chat starts.

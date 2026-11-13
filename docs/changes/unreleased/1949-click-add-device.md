---
kind: fixed
title: Make Add another device clickable with an editable link field
pr: 1949
surface: [chat, docs]
invalidates:
  - The add-device heading was keyboard-only; clicking it now opens pairing instructions.
  - The pairing prompt was static text; it now accepts clicks, typing, and editable pastes before Enter opens approval.
---

The pairing field keeps its own draft and caret. Existing chat drafts are preserved,
and short windows keep the opened form visible whenever it fits. A field hidden
by resizing releases the keyboard rather than accepting invisible input.

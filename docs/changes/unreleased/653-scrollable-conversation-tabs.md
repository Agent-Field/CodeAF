---
kind: changed
title: Keep many conversation tabs readable with a scrollable strip
pr: 653
surface: [chat]
invalidates:
  - "The conversation strip squeezed remembered tabs into very short labels and kept only eight presentation entries. It now keeps up to 32 in stable order, preserves readable labels, and shows directional scroll controls when there is room. Narrow frames retain the selected tab and Chats fallback."
  - "A wheel over conversation tabs could scroll the transcript below them. Vertical and horizontal wheel gestures now browse the tab viewport without selecting a conversation or touching its draft or reading position. Selecting or closing a tab reveals the selection again."
---

Home keeps its own target before the names. New chat and Chats follow the last
visible tab with small gaps; as the row fills they remain beside the scrollable
viewport at its edge. Arrow targets have the same cell padding and hover feedback as navigation;
plain terminals use pointer dots, reserving brackets for the selected chat.
Header air grows in separate steps at 32, 36, and 40 rows on frames at least 48
columns wide, so growing the terminal does not reduce the reading area.

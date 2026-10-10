---
kind: fixed
title: Desktop place navigation uses the canonical Open rail and preserves saved tabs
surface: [desktop, chat]
invalidates:
  - "Closing a desktop place only hid its row locally. It now sends the canonical rail close, preserves saved tabs for reopening, and leaves running work alone."
  - "Open in new window opened a browser tab in browser builds. It now goes to the place in the same window; native windows remain separate."
  - "Home used Command/Control+[ to go up a level. Iteration 2 assigns Command/Control+Up to the primary parent or All places."
---

Go to visits the canonical Open MRU and requests Home focus, including repeated
visits. Close all others keeps the selected place and pinned places. Failed closes
leave the place open; failed soft visits report an error while keeping the view.
A slow destination lookup cannot replace a newer navigation.

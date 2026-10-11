---
kind: fixed
title: Desktop overview previews reuse already loaded content
surface: [desktop, chat]
invalidates:
  - "Opening Filmstrip mounted full workspace panes, attached conversations again and waited for transcript reads. It now renders known summaries synchronously and mounts offscreen preview bodies only when visible."
  - "File, diff and terminal preview reads were discarded on close. Compact content is now shared across the grid, filmstrip and hover previews, cached by resource and summary revision, and primed by normal workspace reads."
---

Closing and reopening All tabs reuses preview content. Unknown content stays
absent; any required resource reads run independently and never hold the frame.

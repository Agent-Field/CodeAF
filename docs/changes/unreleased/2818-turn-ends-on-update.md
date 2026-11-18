---
kind: fixed
title: A reply that stops on its own progress update is sent back to do the work it announced
pr: 2818
surface: [chat, engine]
invalidates:
  - "A turn ended on any reply that made no tool call, including one the model opened with `[update]` — the marker the system prompt reserves for interim notes between tools. Such a reply is now sent back to work with the notice `it sent a progress update and stopped · asking it to go on`, at most twice a turn, unless it ends by asking the person a question (`?`, `？` or `؟`) or is waiting on work already out — a running background command, watch or render, a handoff to a live task, or a task still out. After the second, an update is an ordinary reply: a typed turn ends on it and a woken one may still be read for what remains."
  - "The end-of-turn reader's question check knew only the ASCII `?`. It now also reads the full-width `？` (U+FF1F) and the Arabic `؟` (U+061F), so a reply ending on a question in Chinese, Japanese or Arabic is waiting on the person everywhere that check is used."
  - "The end-of-turn reader was the only thing that could re-open a stopped turn, and it never reads a turn the person typed. The update nudge runs before that gate, costs no reader, and applies to typed and woken turns on every model."
---

Seen on the connected-canvas eval in runs 3, 4 and 5: deepseek-v4.1-flash wrote
"[update] … Next I write each slide's HTML in one pass" and the board deck was
left as five blank frames.

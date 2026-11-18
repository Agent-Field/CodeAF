---
kind: fixed
title: A reply that stops on its own progress update is sent back to do the work it announced
pr: 2818
surface: [chat, engine]
invalidates:
  - "A turn ended on any reply that made no tool call, including one the model opened with `[update]` — the marker the system prompt reserves for interim notes between tools. Such a reply is now sent back to work with the notice `it sent a progress update and stopped · asking it to go on`, at most twice a turn, unless it ends by asking the person a question."
  - "The end-of-turn reader was the only thing that could re-open a stopped turn, and it never reads a turn the person typed. The update nudge runs before that gate, costs no reader, and applies to typed and woken turns on every model."
---

Seen on the connected-canvas eval in runs 3, 4 and 5: deepseek-v4.1-flash wrote
"[update] … Next I write each slide's HTML in one pass" and the board deck was
left as five blank frames.

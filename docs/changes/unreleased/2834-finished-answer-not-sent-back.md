---
kind: fixed
title: A finished answer written right after codeaf asked for a note is no longer sent back
pr: 2834
surface: [chat, engine]
invalidates:
  - "Since #2831, any tool-free reply that came straight after codeaf's `[silent]` note or `[held]` demand was sent back with `it wrote down where it was and stopped · asking it to go on`, read from its position alone. Now the reply must also be short: one or two plain paragraphs. A reply of three or more paragraphs, or one with a list item, heading or table row, is an answer laid out for the person, and the turn ends on it. The cap and the budget shared with the update nudge are unchanged."
---

In qloop run 9 the send-back fired five times and every one was the agent's
finished summary, which it then answered with `[no change]`. Across the
baseline to run 9, all sixteen finished answers after the request were laid
out (three or more paragraphs or a list), and three of the four unmarked stalls
were one or two plain paragraphs. The fourth, a stall that wrote its plan as a
numbered list, now ends the turn as it did before #2831.

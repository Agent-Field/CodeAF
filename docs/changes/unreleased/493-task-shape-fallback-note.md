---
kind: fixed
title: a shaper that spends its whole window thinking starts the task unshaped with a note
pr: 493
surface: [engine, chat]
invalidates:
  - "TaskStart carried (id, title, err) — it now carries a third field, note, and every StartTask caller (tui3, remote wire, e2e harnesses) destructure four values."
  - "A cut shaper used to leave the task unshaped with nothing said. The note — 'brief kept as you wrote it' — is now carried on taskStartedMsg and drawn beside the started line, dim, in the slot app.noteFacts already carries."
---

When a shaper's shape window was spent entirely on thinking, the task started
with the brief as written and nothing on the record: no shape, no line saying
why. #133 was silent by construction — the code path had nowhere to put a word.

The fallback note rides the wire: session.StartTask returns it, remote forwards
it on the task-start wire, and the chat draws it dim beside the started line,
reusing the note slot the stopped-turn line already uses. Empty is every
ordinary start, and the absence law draws nothing for it.

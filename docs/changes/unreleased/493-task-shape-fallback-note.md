---
kind: fixed
title: a shaper that spends its whole window thinking starts the task unshaped with a note
pr: 493
surface: [engine, chat]
invalidates:
  - "session.StartTask and remote.Agent.StartTask returned (id, title, err) — they now return (id, title, note, err), and every caller (tui3, the remote server's task door, the e2e harnesses) destructures four values. remote.TaskStarted grew a Note field, omitempty, additive on the wire; the protocol Version stays 10 because an engine that never sets it is read as an ordinary start, which is what it is."
  - "A cut shaper used to leave the task unshaped with nothing said. The note — 'brief kept as you wrote it' — is now carried on taskStartedMsg and drawn as its own dim line under the started line, in the slot app.noteFacts already carries. It is said ONLY when a shaper ran and was cut; no model resolved for the shaper role, and an answer that came back whole but did not parse, both stay silent as before. internal/manual/chat/tasks.md now carries the line."
---

When a shaper's shape window was spent entirely on thinking, the task started
with the brief as written and nothing on the record: no shape, no line saying
why. #133 was silent by construction — the code path had nowhere to put a word.

The fallback note rides the wire: session.StartTask returns it, remote forwards
it on the task-start wire, and the chat draws it as its own dim line under the started line,
reusing the note slot the stopped-turn line already uses. Empty is every
ordinary start, and the absence law draws nothing for it.

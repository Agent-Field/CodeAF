---
kind: fixed
title: tool-grammar leaks are cut and rerouted, and /model says what it leaves running
pr: 131
surface: [chat, engine]
invalidates:
  - A serving endpoint that leaks the model's chat-template tool tokens as text no longer reaches the screen or the transcript; before 2026-08-31 that reply passed every guard and the turn re-asked into its own markup forever.
  - The reply guard now owns four failure planes, not three; `reply guard off` in /settings also turns the new tool-markup cut off, not only the repetition guard.
  - Switching the conversation's model while tasks run now says "tasks already running keep the model they started on" in the model note; the silent `model · <id>` line alone is gone for that case.
---

A deepseek endpoint served its model without parsing the chat template, and the
model wrote its `web_search` call as visible text — a healthy-looking 200 with
no structured tool calls that no existing guard owned. The provider now reads
the finished reply's shape at both epilogues (a declared tool's name fenced in
symbol runes inside symbol-dense text, no code fence anywhere) and ends the
call as a cut: the lane is struck, the endpoint pin released, and the session
ladder re-asks once and then walks the fallback chain. Detection is structural,
never a pattern for any one provider's syntax.

Separately, the same incident showed /model mid-task telling the person
nothing: a task's model is frozen at admission by design, and the switch note
now says so at the moment it matters. The manual answers both in the asker's
own words.

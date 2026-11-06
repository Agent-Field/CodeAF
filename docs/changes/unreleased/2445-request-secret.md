---
kind: added
title: The chat asks for a password or token through a masked box and never sees the value
pr: 2445
surface: [chat, engine, app]
invalidates:
  - "Was true: a model's `ask` with `input.secret: true` drew a masked box, but the typed value came back to the model in the answer JSON, so it entered the transcript and went to the provider. Is true now: the value is written to a 0600 file in `~/.codeaf/secrets/` for 24 hours and the model is told only the path, the character count and the expiry."
  - "Was true: there was no way to hand the chat a credential except pasting it into a message. Is true now: `request_secret` (shelf group `secrets`, interactive conversations only) asks for one; a command that prints the whole value gets `[redacted]`."
  - "Was true: the app's asks carried no credential box and every answer went by `asks.answer`. Is true now: an ask may carry `secret {placeholder, keeps_for_s}`, answered by `asks.answer_secret {agent, id, secret}`; `asks.answer` and the Inbox refuse a secret's words."
  - "Was true: the terminal's draft autosave and answer receipt held whatever was typed into a question's box, a connect key included. Is true now: a secret box is never written to the draft file and its receipt says `given, kept out of chat`."
---

Only the whole value is redacted; printing part of a secret is forbidden by the tool's instructions, not caught mechanically.

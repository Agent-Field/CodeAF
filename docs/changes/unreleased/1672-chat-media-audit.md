---
kind: fixed
title: chat sign-in cancel, picture names, speech spend, palette lists and team wording are accurate
pr: 1672
surface: [chat, engine, docs]
invalidates:
  - "Angle-bracketed words like `<id>` in model prose were dropped. They now show as written."
  - "The composing model list in `/settings` was empty because Lyria's audio row was rejected. Audio rows marked as music are listed now; plain speech rows still are not."
  - "`/remember`, `/memories` and `/forget` said memory is off on a hosted session that has it on. They now use the connected engine's store and setting."
  - "A redial's ssh diagnostics were painted over the full-screen frame, and a redialing window came back as a watcher of its own session. The frame stays clean, and a returning window takes back its own keyboard without taking another live window's."
  - "An empty `/drafts` trapped the keyboard, and `/skill` on an empty shelf left `/skill ` in the message box. Both now behave like every other place."
  - "Esc did not cancel a browser sign-in started from `/connect` or the Codex row. It does now."
  - "A generated picture was named from the provider's declared type, so JPEG bytes could be saved as `.png`. It is named from its bytes now."
  - "Speech spend had no role in `usage.jsonl` and media calls left no call-log rows. Speech records under its own role and every media request writes a call-log pair with its cost."
  - "The `/` list and `/help` left out `/land`, `/crew cap task` and `/cache clean now`, `/compact` on a short chat printed a raw engine error, and a fallback task title could end on a dangling word. All three are fixed."
  - "`team_start` told the model the person is always asked first, and a sub-team manager naming its own team in `team_post` got an unknown-team refusal. The description now says when the person is asked, and the refusal points at the team above."
---

Re-cut from the larger audit batch so each fix reviews on its own.

---
kind: fixed
title: chat sign-in cancel, picture names, speech spend, palette lists and team wording are accurate
pr: 1702
surface: [chat, engine, docs]
invalidates:
  - "Angle-bracketed words like `<id>` in model prose were dropped. They now show as written."
  - "The composing model list in `/settings` was empty because Lyria's audio row was rejected. Audio rows marked as music are listed now; plain speech rows still are not."
  - "`/remember`, `/memories`, `/forget` and the memory place said `memory is off for this session` on a plain `codeaf` in a folder (the engine road) and on a hosted session, while Settings said memory on; the demo home's notes were hidden. They now use the connected engine's store and setting, follow the engine's automatic profile, run off the update loop so a slow engine does not freeze typing, and apply in the order they were issued. A save that outlives the wire wait says it has not answered yet instead of claiming failure."
  - "A redial's ssh diagnostics were painted over the full-screen frame, and a redialing window came back as a watcher of its own session. The frame stays clean (the first carrier too, once its handshake is done), and a returning window takes back its own keyboard without taking another live window's."
  - "An empty `/drafts` trapped the keyboard, and `/skill` on an empty shelf left `/skill ` in the message box. Both now behave like every other place."
  - "Esc did not cancel a browser sign-in started from `/connect`, the Codex row or the Settings Codex row. It does now; in Settings the first esc cancels and keeps the page open."
  - "A generated picture was named from the provider's declared type, so JPEG bytes could be saved as `.png`. It is named from its bytes now."
  - "Speech spend had no role in `usage.jsonl` and media calls left no call-log rows. Speech records under its own role and every media request writes a call-log pair with its cost, including header-only image costs and a completed video poll's price."
  - "The `/` list and `/help` left out `/land`, `/crew cap task` and `/cache clean now`, `/compact` on a short chat printed a raw engine error, and a fallback task title could end on a dangling word or mid-word. All three are fixed."
  - "`team_start` told the model the person is always asked first, and a sub-team manager naming its own team in `team_post` got an unknown-team refusal. The description now says when the person is asked, and the refusal points at the team above."
---

Re-cut of #1672 (itself re-cut from the larger audit batch) onto current dev.

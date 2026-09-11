---
kind: fixed
title: The questions wave is driven end to end against a real model, and six gaps it found are closed
pr: 765
surface: [chat, engine, docs]
invalidates:
  - "A question raised behind the ordinary door reached nobody. `aforge` and `aforge chat` in a project attach this machine's session host (#736), and the question object does not cross that link in either direction — no block, no chip, and the turn waits. It is still true; the manual now says so, and a conversation you want to be asked in is opened with `aforge chat --no-host`."
  - "`internal/manual/chat/questions.md` said every lane can be answered from a row on home. A question the model raised with `ask` is shown on that row and no key over it takes it; `enter` brings the conversation here instead. `home.md` carried the same claim and now carries the same correction."
  - "The manual described `[u] undo` on a ratify line as offered whenever there is something to undo. Nothing marks a ratify row's work as still undoable, so it is never drawn; `c change` is the way back."
  - "An answer given in the question room was recorded as `another window` — the room resolved through the engine's door without telling the block, so the answer coming back was read as somebody else's key. It says `you` now."
  - "`space tick it` was offered on every room and given up sixth for width, so a checklist at a hundred columns drew as a plain list with no verb that works it. It is now offered only on a checklist and ranked with the answers; `tab next blank` is offered only where there is more than one hole."
  - "`s` on the sheet sent every answered row and left no record at all. Each row now leaves the same dim `decided …` line an answer given on its own leaves."
  - "A session stopped on the model's own `ask` wrote `working` into its presence file, so home and every other window drew it as busy. `waitingOnPerson` now counts that lane, and the row reads `<name> asks: <what was asked>`."
  - "The headless line `asked: <head> → 1 (default · nobody to ask)` was built and never printed: it rode the answer's `From` field and nothing read it. `aforge chat --once` prints it on the error stream now, beside the `tool:` lines."
---

`internal/e2e/questions_e2e_test.go` is the proof: sixteen scenarios that start
tmux, steer `deepseek/deepseek-v4-flash` into one `ask` call of the shape each is
about, and read the screen back. It photographs every moment it asserts on, and
`docs/design/questions/GALLERY.md` is those pictures with the keys that got there.

Six of the scenarios are red on purpose. A red one is a finding with a picture
attached rather than a thing to work around, and none of them is skipped: the
session-host link, answering from a row on home, the ratify row's undo, the
assumption kind's own mark and sentence, the room drawing two lines for one
decision, and the older blocks — the approval gate among them — which still keep
their own keys and still spell `esc` as `cancel`.

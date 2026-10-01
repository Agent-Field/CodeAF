---
kind: changed
title: the first run takes clicks and a second enter, shows no telemetry notice, and lists free models only
pr: 1720
surface: [chat, docs]
invalidates:
  - "On the first conversation's screen, enter on a starting point filled the box and the next enter did nothing — the starting point kept taking it. With words in the box, enter now sends them: ↓ enter enter sends the starting point's sentence."
  - "The Models and spending setup screen swallowed every mouse press. A press on a row is now the key that row would take — the limit focuses, the chat model opens its list, a model in the list is taken, the review shows, `Start a conversation` leaves — and the wheel scrolls the open list."
  - "Enter on the chat-model row left the focus there after a model was taken, so the next enter reopened the list; enter on an open review folded it away. Both now go on to the next row, so enter alone walks the whole form down to `Start a conversation`."
  - "codeaf printed a six-line anonymous-usage-counts notice once per install — on the first conversation's screen under the starting points, or to stderr ahead of `chat --once` and task commands — and sent nothing until a frame or a terminal had shown it. It prints no notice anywhere now and the gate is gone; the disclosure is the README's Telemetry section and docs/TELEMETRY.md, which a test holds to the switches."
  - "`codeaf telemetry` (`status`, `info`, `show`, `on`, `off`) existed and `--help` listed it. It does not exist; `codeaf telemetry` is an unknown command. The switches are unchanged: the `telemetry` toggle in /settings, CODEAF_TELEMETRY=off, DO_NOT_TRACK=1, `telemetry = off` in the project file, an empty CODEAF_TELEMETRY_ENDPOINT. tui3.Options no longer has TelemetryNotice or TelemetryNoticeShown, and internal/telemetry no longer has Notice, PrintNotice, NoticeShown or MarkNoticeShown."
  - "The setup screen's chat-model list was the whole catalog whatever the account's balance. While the OpenRouter balance reads low ($0.50 or less, the reading behind the low-credits warning), it offers only the `:free` ids and the catalog rows priced at zero, its count line says `free only`, and `Your OpenRouter account is low on credits · the list shows free models only` stands under the field. The model in use stays on the list."
---

Santosh watched five new people through the first run on 2026-09-30. Four read
the second enter doing nothing as enter being broken, most read the telemetry
notice as something to deal with, and the ones on an empty OpenRouter account
picked paid models they knew by name from a list that should not have offered
them. The notice's legal job is done by the repository, so it left the product
with the command that explained it.

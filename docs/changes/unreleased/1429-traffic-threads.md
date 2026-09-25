---
kind: changed
title: team Traffic is threaded, the newest thread at the top, and a manager's question keeps its answers
pr: 1429
surface: [chat, engine, docs]
invalidates:
  - "A message to several members was one Traffic entry per member. team_send takes several handles and writes one entry (to several, with handles); each named member is still told once."
  - "Traffic entries stood alone. An entry carries answers, the id of the entry it answers: a member's reply to the manager answers the last message the manager sent it, or the one it names with team_post's thread, and its finished, failed, asking and wake events answer the same one. Members are told each line's number, `◆ directive from manager #42: …`, and team_send's answer carries it."
  - "The Traffic rail was a log, newest at the bottom, one row per entry, wakes and finishings included. It is threads, the newest activity at the top: a header, the message on its own line, and one tree row per member, with wakes shown as working…, finishing as ✓, failure as ✗ and asking in the needs-you amber. Handles on it are links; a message's words expand on a press."
  - "A manager's team_send row was a one-line tool call. It reads `team_send ◆ to @a @b · do` with the message quoted under it and each answer attached under that as it arrives, and a turn with one in it is not folded into a work chip. The member's chat hangs its own answers under the manager's card, and in the manager's chat a delivery note whose answers are already under their card is one dim line."
---

The thread link is one JSON field on the entry, so it crosses --host with the
entry, and entries from before it read as threads of their own. The rail and
the chat cards draw from the Traffic cache the rail's clock keeps, keyed by
that cache's version; no frame reads the log.

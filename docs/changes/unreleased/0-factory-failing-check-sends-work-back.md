---
kind: changed
title: A failing check sends the work back to the chat before it, up to the check's max rounds
pr: 0
surface: [engine, docs]
invalidates:
  - "Was true: a check step that failed ran the same command again in its next round, which fails the same way, until its rounds were spent and the person was asked. Is true now: when a check's round fails and the finished step before it is a chat, the work goes back to that chat with the failure as its note (the command, how it ended, and the last 20 lines it printed), the chat runs another round, and the check runs again after it, as a person's send-back from an approve step does. The check keeps its own count, so its max bounds how often the work goes round; once it is spent the person is asked, as before. A check with no chat before it (first in its pipeline, or after another check) still runs again on its own. Nested loops are not part of this."
---

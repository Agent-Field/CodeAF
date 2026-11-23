---
kind: fixed
title: codeaf app keeps a member's answer an answer, and says its deliveries and posts as facts
pr: 0
surface: [engine]
invalidates:
  - "A reopened conversation called a message an update when ANY later tool call came before the person's next line, so a member's answer (\"BIRCH-9304\") became an update once a manager's directive opened the next turn. Now a message is an update only when its own calls ran after it, as the live pump says."
  - "A crew delivery reached the app only as worded text (\"manager: …\"). Its notice now carries `team` lines: from, to, kind (note · directive · start) and the log entry."
  - "A team_post call said where it went only in its answer's words. Its tool entry and turn.tool now carry `handoff`: crew, recipient, the log entry it became, what it answers, and delivered false with the engine's words when it was refused."
---

`appEntryShapes` names updates per message (`appUpdates`). `session.ReadTeamPost`
reads back the sentence `team_post` answers with; the two writers now compose it
through the same file (`teampost.go`), so the reader cannot drift from them.

---
kind: added
title: a team can have a manager chat, with a Traffic rail of who told whom
pr: 1429
surface: [chat, engine, docs]
invalidates:
  - "A team was a grouping and nothing more. It can have one manager conversation (`+ Manager` on the strip, or Make this team's manager in the switcher), which runs the team through team_status, team_read, team_send, team_stop and team_start; members reply with team_post."
  - "A conversation started by a tool came to the front. A member the manager starts with team_start opens behind the conversation in front, as a tab named @handle, and takes its first turn on its own; nothing a manager does moves the person's focus."
  - "A brief handed to a new conversation arrived as the person's first message. The manager's brief reaches the member through the team's Traffic, marked as the manager's, and its page draws it as a quoted card headed by the manager."
  - "The right-hand column was the task column's alone. While a team's manager is in front the Traffic rail holds it and the task column folds to its edge; alt+l shows or hides the Traffic and alt+m goes to the manager."
  - "A shown team drew every member on the tab strip, open or not, while the conversations view kept only the open ones. Both are now what is open in this window, narrowed by the team; the view's title says `open in this window · in test` and, while the team has members not open here, offers `2 more in test · Open them` (r), which resumes them behind the conversation in front."
  - "A member's handle was made from its title's words and never changed, which made @review of a security review. The title model now chooses one word naming the subject, once, when the title is made; a handle a person or the manager gave is kept, and each change is written to the team's Traffic as `@review is now @security`."
  - "An @handle or a team's name in a chat was plain text. Each handle of a member of the conversation's teams, and a team's name written as a team, is a link: a ground under the pointer, the member's title on the hint line, and a press opens or resumes that member, or shows the team."
  - "Over --host the teams list was the laptop's own. It is the engine machine's now, read and written over three wire methods (Teams.Read, Teams.Update, Teams.Traffic), so a manager and its Traffic work over --host; against an engine without them teams and the manager are off rather than kept on the laptop."
---

The UI and the session meet only through internal/teams (teams.json and each
team's traffic.jsonl), which the UI reaches through a seam: locally the
profile, over --host the engine's profile over the wire. A quiet window with a
managed team costs one stat per file a second (over --host one small round
trip per file), backing off to one in five seconds, and draws no frame.

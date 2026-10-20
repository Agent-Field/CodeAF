---
kind: added
title: + New from this session makes this chat a team's manager, and /helpers asks it for helpers
pr: 1881
surface: [chat, engine, docs]
invalidates:
  - "Every way to make a team started from the team, and a team was made without a manager; the only way to make an existing conversation one was Add chat, then Choose AI manager. The Teams ▾ dropdown's `+ New from this session` makes a new top-level team with the conversation in front as its manager the moment it is chosen, in one save, selects it, and puts `/helpers` and a one-sentence default ask in the box."
  - "`/helpers` asks the team the conversation in front manages for helpers: it sends exactly the words after it (bare, the default ask, shown in full) as the person's message, and makes no team. In a conversation that manages none it changes nothing and says to make one with + New from this session."
  - "The Teams ▾ chip was absent with no teams, and its dropdown printed an up/down, enter, esc footer. The chip shows with no teams, the footer is gone, and with no team selected the dropdown ends with `+ New from this session`. With a team selected the row is absent: the team it makes is top-level, and nesting is + Add subteam."
  - "A manager had no way to take its team back. `team_dissolve` forgets a team that has never held anyone but its manager (no member, former member, subteam or Traffic); it is on the belt only while that is true, for every team however it was made, and does not ask first."
  - "A member a manager started with team_start never took its first turn on an ordinary launch: the engine's teams door stamps a new member's join time, the join bound dropped the start that made it, and the member sat idle with nothing addressed to it. A member's own start is exempt from the bound now, and it wakes with its brief; every other line from before the join is still withheld."
  - "There are twenty-five tips, not twenty-four: `+ New from this session in the Teams menu makes this chat a team's manager`."
  - "An interactive conversation in no team is now told once, after the person's fourth message, that it may suggest + New from this session when the work is long and has parts a few helpers could take on at once. It is a note, not a paragraph of the system prompt."
---

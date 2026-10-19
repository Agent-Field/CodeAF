---
kind: added
title: /helpers makes the conversation you are in a new team's manager, which proposes helpers
pr: 1881
surface: [chat, engine, docs]
invalidates:
  - "Every way to make a team started from the team, and a team was made without a manager; the only way to make an existing conversation one was Add chat, then Choose AI manager. `/helpers` makes a new top-level team with the conversation in front as its manager, in one save, then sends exactly the words after it (bare, a default ask shown in full) as the person's message."
  - "The Teams ▾ chip was absent with no teams, and its dropdown printed an up/down, enter, esc footer. The chip shows with no teams, the footer is gone, and with no team selected the dropdown ends with `+ New from this session`, which puts `/helpers` and the default ask in the box. With a team selected the row is absent and `/helpers` refuses: the team it makes is top-level, and nesting is + Add subteam."
  - "A manager had no way to take its team back. `team_dissolve` forgets a team that has never held anyone but its manager (no member, former member, subteam or Traffic); it is on the belt only while that is true, for every team however it was made, and does not ask first."
  - "A member a manager started with team_start never took its first turn on an ordinary launch: the engine's teams door stamps a new member's join time, the join bound dropped the start that made it, and the member sat idle with nothing addressed to it. A member's own start is exempt from the bound now, and it wakes with its brief; every other line from before the join is still withheld."
  - "There are twenty-five tips, not twenty-four: `/helpers makes this conversation the manager of a new team`."
  - "An interactive conversation in no team is now told once, after the person's fourth message, that it may suggest /helpers when the work is long and has parts a few helpers could take on at once. It is a note, not a paragraph of the system prompt."
---

---
kind: fixed
title: wall tiles say what each conversation spent, and the teams surface stops saying untrue things
pr: 1516
surface: [chat, engine]
invalidates:
  - "A wall tile drew no spend, although #1429 promised it. A tile's state line now ends in the figure that conversation's own status line shows (the work it started included), and draws nothing when it spent nothing."
  - "A team write the store refused (a lock, an unreadable file, a far machine that kept changing) still drew `Made beta · 1`, `Organized · …` or `harbor is closed`. Those notices now wait for the write that carried their edit, and a refusal takes their place: `beta was not saved · <reason>`."
  - "Wall tiles, and a reopened manager conversation, drew the wake sentence (`Your team's replies started this turn; the person did not speak. …`) that the live conversation never draws. Neither draws it now; a team delivery draws as its lines."
  - "A team cap under a cent read `$0.00` and offered `Raise to $0`, whose stored ceiling lifted nothing, and the settings card said `$0.0010 a day`. Every place now spells a cap one way (`$5`, `$5.50`, `$0.001`), and Raise always offers twice the ceiling and names it exactly. The card spells a whole-dollar cap `$5`, where it said `$5.00`."
  - "A team that was wrapping up showed no deadline anywhere. The teams page header and the manager's side column now say `wrapping up · 12m left`, then `under a minute left`, then `out of time`."
  - "On a plain local launch, a closed team's pane said its report `is not readable over this connection`. The report now opens there; only over --host does that sentence remain."
---

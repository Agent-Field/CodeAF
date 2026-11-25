---
kind: changed
title: A task an agent paused to ask reads as asking you, and Tasks never draws a zero
pr: 2811
surface: [app, docs]
invalidates:
  - "A Tasks row for an agent paused on an Ask drew the working dot and a time (\"3h\") and read as running. It now draws a hollow ring and \"asks you\" (or \"asks Marco\"), and is counted only under Waiting on you or Waiting on others."
  - "A heading like \"1.0 launch\" sat under Waiting on you with the waiting task inside it. A task waiting on you is now its own row under Waiting on you, with its group's name; the group stays with its other tasks."
  - "Up next was always folded under Open, so a workstream whose tasks were all Up next showed one line. The first section the list draws now starts open."
  - "Tabs and state chips drew zero counts (\"Tasks 0\", \"Running 0\"). A zero is drawn as nothing, and an empty chip is hidden except Open and the chip you are on."
  - "Below 1100 of content width, or with Afi open, Control-click opened a task in place. It now selects, the selection pane stacks above the list, and the pane opens with \"Give these N to Afi\"."
---

Desk Agent-Field/codeaf-desk#641 made these changes. This entry updates the chat manual so it describes them.

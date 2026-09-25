---
kind: changed
title: a run's rows on the rail and its page look like every other task
pr: 1420
surface: [chat]
invalidates:
  - "A run's rows on the rail had a renderer of their own: a still half-circle where every other working row wears the spinner, no #id, no clock and price line, and a family's finished parts folded into one `✓ N done` count with a dot row and a summary sentence under the run. Every run row and every part is now drawn by the node rows' own renderer: the spinner, the `#id` (the store's own for a part, such as `#k3x9qa`), `4m · $0.02` under a running row with each figure left out when unknown, and every part its own row in the old tree. Tokens and the model are not in the run store, so a run's rows never show them."
  - "A node row that carried a run used to be dropped when a plan row with the same title was drawn. It is kept, drawn as the head of its family, and the run's parts hang under it."
  - "A run's task page opened on a bold title over a plain rule, with a telemetry line in its body, numbered steps and a `◐ $ <command>` live row. It wears the task room's head now — the trail `<conversation> ▸ <task>` with `esc back`, and the facts rule led by the state's spinner, then the clock, the steps and the running and queued parts, with the price at the far end. Steps are the room's shell rows (`$ <command>`, the head of what came back under it) and carry no numbers; the parts under it are the rail's rows. The `esc/← <parent>` line is gone: the parent is on the trail."
  - "A part that had landed, or had a live step with no command, drew `◑ $` with nothing after it on its parent's page. A live line with no command is no line."
---
The adapter is internal/tui3/planrail.go: a store row is lent a taskNode holding
only what the store knows, and app.taskStatus reads a lent node through
planStatus, so the mark, the group and the under-block are the ones every node
row gets. The run's summary sentence is still read and still drawn on the tasks
place; only the rail stopped drawing it. The note box, `enter send`, `x stop it`
and `esc back` on the page are unchanged.

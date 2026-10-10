---
kind: changed
title: Desktop drilled headers name their parent
surface: [desktop, chat]
invalidates:
  - "Task headers had a permanent Back icon followed by the full ancestor trail. They now start with the immediate parent's return button and the current title, matching Iteration 2."
  - "There was no temporary strip return chip primitive. BackChip now provides the measured return affordance and dismisses on the next click or key elsewhere; the focus-history lane still owns mounting it for unsolicited jumps."
---

Shared controls and component tokens preserve light/dark appearance and keyboard
focus. The existing strip leading slot is the focus-history integration seam.

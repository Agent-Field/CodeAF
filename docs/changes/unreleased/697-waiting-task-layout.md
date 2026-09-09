---
kind: fixed
title: Keep task details inside their column and short task windows usable
pr: 697
surface: [chat, docs]
invalidates:
  - "The pinned child summary used the full window width above the roster. It now fits the shared task body width, with the same calculation used for drawing and height accounting."
  - "An empty waiting task page printed the bare reason, such as its parts. It now uses the roster's complete waiting explanation."
  - "The manual claimed deeper delegation costs more than it saves. The two-level cap is a chosen bound without a three-level benchmark; both propose_task and tasks are absent at that depth, and the manual now names that existing behavior."
---

Long child titles are covered by frame regressions and a real tmux run using
DeepSeek Flash, including opening a child and reading the parent’s completed or review state.
The live run also exposed a negative standing-section row count that crashed a
short task window with no standing orders; the count now stops at zero.

The terminal harness waits for an interactive surface before typing and preserves
crashed panes for diagnosis. The task record test follows the current conversation grouping before selecting
a task and opening its saved record.
Delegation limits and access to other tasks are unchanged.

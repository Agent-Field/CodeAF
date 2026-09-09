---
kind: fixed
title: Keep waiting task details inside the task column
pr: 697
surface: [chat, docs]
invalidates:
  - "The pinned child summary used the full window width above the roster. It now fits the shared task body width, with the same calculation used for drawing and height accounting."
  - "An empty waiting task page printed the bare reason, such as its parts. It now uses the roster's complete waiting explanation."
  - "The manual claimed deeper delegation costs more than it saves. The two-level cap is a chosen bound without a three-level benchmark; both propose_task and tasks are absent at that depth, and the manual now names that existing behavior."
---

Long child titles are covered by a frame regression across wide and narrow windows.
Delegation limits and access to other tasks are unchanged.

---
kind: fixed
title: an organization's task names its people by the names they joined with
pr: 2692
surface: [factory, app]
invalidates:
  - "Was true: a task on an organization's server named its owner and doer only as the desk sent them, so a task made with `owner: {kind: person, id}` answered with an empty `name` and the desk read \"Owner undefined (you)\"; a hand-off named the new owner by handle. Is true now: every answer carrying a task names `owner`, `done_by`, `came_from` and `created_by` people by the name they joined with, or the handle when the organization knows none."
---

Live use L3 F-14 and L1 F17. The stored task is unchanged; the name is read
from the organization's members when the task is.

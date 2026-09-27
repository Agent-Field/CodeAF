---
kind: fixed
title: Held tasks stop cleanly and recover their exact working folder
pr: 1604
surface: [engine, chat]
invalidates:
  - "A machine-held task could hold a folder and could not be stopped from the main box."
  - "Restarted ordinary runs and joined tasks could remain working or appear finished without completing."
  - "Admission could miss settings changes made before creating a task or in the default profile."
---

Queued runs retain their exact folder, mode and brief before admission. They
allocate no working copy or folder lock until admitted, and stop from the task
room or the main box. Reopening restores the same queued request or adopts an
ordinary run's recorded copy. Joined rows follow the owner's admission state;
stopped children and recorded terminal results retain their ending.

Incomplete legacy records remain visibly interrupted with an actionable reason.
Program runs keep their existing recorded-exit settlement, and ordinary recovery
does not promise exactly-once replay of arbitrary external operations.

Graph and run admission read current persisted limits when created and follow
later changes from another process, including the ordinary default profile.
Absent settings preserve the supplied startup limits.

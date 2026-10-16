---
kind: added
title: video and music renders have a durable, complete history the app can list, look up and stop
pr: 0
surface: [engine, remote]
invalidates:
  - "Was true: a render existed only as a job row and prose notes, so an app could not list past renders, could not tell a failed one from a finished one without reading text, could not stop one except through the job number, and knew nothing of its charge. Is true now: every render has a stable id and a typed record (state running, finished, failed, cancelled or interrupted; model, start, duration, the run and call that asked for it, whether its bytes were committed, the saved path, and the provider's charge as charged, not_charged or unknown) kept in an append-only history that is never cut off, so an old render resolves by id after any number of later ones and after the conversation is reopened. render.list, render.get and render.cancel serve it, and render.changed says each start, ending and late charge once. A render stopped before it committed stays cancelled with no output even if the provider ignores the stop and later reports a charge; the charge is added to the same record rather than as a second ending. An older host answers unknown_method, never an empty list. Stopping tells the provider nothing, so a cancelled render says the provider's state is unknown."
---

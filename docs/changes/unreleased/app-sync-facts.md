---
kind: fixed
title: Desktop chats show sync moves and acknowledge the hosted sync disclosure
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "App sync notices used to wait on a terminal-only desk; they now reach the affected desktop chat as visible local notices without writing to its synced journal."
  - "A desktop chat displaced by another computer used to accept messages until a tool met its gate; the app now refuses new user work before submission and preserves queued drafts and staged attachments."
  - "Starting a hosted drive in app mode used to mark the disclosure told before any desktop showed it; only an explicit acknowledgement now writes the shared sync-told marker."
  - "Local terminal takeover requests used to leave app-held journals locked; the app now releases them and reports the move, including during a reply."
  - "The app used to drop crash-interrupted command warnings with the opening welcome; each warning now reaches the chat as a visible interrupted notice."
---

Adds the optional appwire features `sync.status` and `chats.location`,
`sync.get`, `sync.acknowledge`, `sync.changed`, `Agent.location`, and the
typed `held_elsewhere` conflict. Reads remain available on a viewer. Queue
edits and reordering only maintain the queue; sending checks ownership again.
Task and program actions check before recording an action intent; replaying an
existing receipt stays available. The session reuses its drive's viewer gate
for remote messages, task starts, work-resuming answers and queued follow-up
draining, including image submissions. Ownership refusals keep the engine's
sentence and device facts across the remote connection.

Never-paired homes remain quiet and explicit sync off stays off. The terminal
keeps its surface notice desk and its existing first-run disclosure behavior.
Its input now also refuses work on a viewer through previously unguarded
session doors; ordinary admission reads only the local viewer fact.

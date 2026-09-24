---
kind: fixed
title: run workers wait for their step to be recorded and their notes delivered before acting again
pr: 1410
surface: [engine, docs]
invalidates:
  - "The run read tool-end events asynchronously while its worker kept asking the model for more actions. A slow record could let the worker race past its step cap or execute many actions before a note was delivered. Run workers now acknowledge each completed action after recording it, applying limits and handing over notes, before the next action can start. Cancellation releases a worker if its event reader fails. Ordinary conversation streams remain asynchronous."
---

The note-channel CI failure could be reproduced by delaying the trajectory
recorder: the scripted worker executed more than twelve actions before reading
a note that was already on its task, then hit its nine-step cap. The cap and
notes now share a real boundary with the producer instead of a race against its
event backlog. The existing nine-step test remains unchanged in scope, and its
failure now includes the trajectory and request transcript.

A deterministic scheduler test holds the reader's acknowledgement back and
checks that the producer stays blocked, then checks both acknowledgement and
cancellation release it. A separate test preserves the ordinary asynchronous
path.

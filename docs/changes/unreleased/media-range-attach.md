---
kind: added
title: A span of a recorded audio or video can be staged as a file attachment with typed provenance
surface: [engine, app]
invalidates:
  - "A recorded audio or video could only reach a message as a path in its words, and a range of it had no durable form. media.attach_range now keeps an exact owned snapshot of the recorded file as an ordinary file attachment, and the person's message records the source, its verified version and the from/to milliseconds as typed data beside their unchanged words. It is never a picture, never clipped or decoded, needs a recorded duration, and abandoning an unsent one removes only the snapshot."
---

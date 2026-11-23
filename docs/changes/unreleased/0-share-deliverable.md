---
kind: added
title: The agent can hand a file over as a result with share_deliverable
pr: 0
surface: [engine, chat]
invalidates:
  - "A file the agent produced outside write or a generator — a chart a script rendered, a PDF, an archive — could not become a deliverable. share_deliverable records up to 8 files (64 MiB in all) as origin shared_by_agent under the dispatch's message, run and call, from a durable receipt keyed by the caller's operation_id; a retry with the same id never records twice."
  - "Media origins were attachment, generated, evidence and published. A shared picture, sound or video is now origin shared: never generated, never published, never Made; header facts only, no model, price or change, charge unknown."
  - "Deliverable kinds stopped at markdown, mermaid, image, audio, video, export and document, and origins at agent, saved_by_you and unknown. A shared non-media file is kind file with its sniffed MIME, origin shared_by_agent; deliverables carry call, and original when a task's file was copied into the conversation's artifacts folder."
---

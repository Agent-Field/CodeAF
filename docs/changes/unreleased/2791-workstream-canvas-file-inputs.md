---
kind: fixed
title: Canvas files follow deletion and pages Afi reads become task inputs
pr: 2791
surface: [factory, chat, docs]
invalidates:
  - Organization canvases could remain in live Files after trash or deletion; Files now follows the canvas lifetime and refreshes open lists.
  - A page read through workstream_pages did not become a task input; pages shown to a task run now appear under Uses like files read through ws_file_read.
  - The manual described organization page History and Restore actions; those actions are absent from this release and suggested edits are reviewed on the document.
---

Canvas restore keeps the same file identity and task links. Task canvas previews show the first page through the window's own canvas source in the paired desk change.

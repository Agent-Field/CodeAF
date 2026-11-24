---
kind: fixed
title: A workstream's agent reads an uploaded text file through ws_file_read
pr: 2721
surface: [app, engine]
invalidates:
  - "ws_file_read on an upload answered only its size and type, so an agent had no way to read an uploaded file and went looking for its bytes on disk. Now a text upload answers its words (100 KiB a read, `from` reads on); any other upload says it has no words to read."
  - "Nothing told a workstream's agent that its files are not on disk. The rules it is given now say never to look for them in the app's data folder or its database, never to search the disk, and never to copy them."
---

solo-sheets F1. The desk's Personal door answers the same shape once it re-pins the table.

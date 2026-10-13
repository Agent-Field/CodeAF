---
kind: added
title: The desk app can load chart and picture bytes from an agent
pr: 1800
surface: [engine, chat, docs]
invalidates:
  - "A chart saved by the chat could only be returned as a file path. The binary now serves authenticated image bytes, and the chat is told to save requested pictures as workspace PNGs and include markdown images in its answers."
---

The image route confines resolved files to the agent's workspace or own session
directory, accepts PNG, JPEG, GIF and WebP with matching content, and caps reads
at 10 MiB. The published app contract describes its success as a binary Blob.

---
kind: changed
title: images start collapsed and share explicit preview and original-file controls
pr: 744
surface: [chat, remote, docs]
invalidates:
  - "Sent and generated pictures automatically drew half-cell thumbnails in chat and task pages. They now start as compact text controls and decode pixels only for an explicitly opened preview."
  - "Sent attachments had no individual expansion control. Each image now has preview/collapse and open original actions; only one attachment per message expands at a time."
  - "Opening an image relied on a terminal filename link or the files shelf. The shared media control now offers a direct original-file click and alt+o, while alt+i toggles the last visible image."
  - "The files shelf always handed a path directly to the system opener. It now shares media opening, so hosted paths use the existing fetch-and-mirror route and local attachments retain their machine ownership."
  - "Cell previews were described as a sufficient way to inspect a picture. Transcript and folder previews now explicitly identify their low resolution; full-quality inspection uses the original file in the system viewer."
  - "Expanded preview pixels were inert, and the phone detail sheet swallowed alt+o. Clicking the image or its caption now opens the original, and the phone sheet accepts alt+o or o. Plain SSH explains the local-client route instead of opening a viewer on the server."
---

The screenshot mosaic was a presentation problem on local terminals as well as SSH.
The common media component keeps filename identity, pointer geometry and file ownership
separate from pixel rendering. It introduces no terminal graphics overlay protocol.

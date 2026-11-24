---
pr: 2670
kind: added
title: a workstream's Files — one shared drive on the organization's server
surface: [factory, engine, chat]
invalidates:
  - "An organization workstream kept pages, sources and canvases as three separate things, each with its own tab and its own route. It now also has Files (GET/POST /v1/workstreams/{id}/files, PATCH/DELETE .../files/{fid}, .../files/move, .../files/{fid}/restore and /content; engine factory.org_workstream_files, _file_create, _file_update, _file_move, _file_delete, _file_restore, _file_content; hello word org_workstream_files): folders, documents, canvases, uploads, links, repos and shortcuts in one list. A file points at what it holds — a document IS a page, a canvas IS an organization canvas — so nothing is copied."
  - "A workstream's pages, sources and canvases are no longer what a newer client reads as its drive. The first read of its Files folds every one of them in, once, and gives a workstream with no decisions page a Decisions document carrying the decisions it had; later reads fold in whatever an older client adds through the old routes, so nothing is lost and both kinds of client see the same drive."
  - "Removing a page deleted it at once. Deleting a file moves it to Recently deleted for 30 days (?deleted=1 lists them) and restore brings it back, a folder with exactly what it took; only after 30 days is a document's page let go."
  - "An organization server kept no uploaded bytes for a workstream. Uploads (at most 25 MiB) are now kept content-addressed under factory-workstreams.blobs/ in the server's root."
---

A `workstream.changed` for a file names `part: "files"`; a change to a file's
contents (`edited: "person"|"agent"`) also carries `flagged`, the tasks in the
file's `used_by`, so the tasks lane can mark the running ones. A child
workstream reads its parent's files in `from`, read-only, once the parent field
lands. `person_edited` is what an agent's tool reads before writing a document
directly: it may only when an agent made the file and no person has edited it.
The chat manual's new page `workstream-files` says all of this in a person's words.

---
kind: changed
title: an organization's files move to another workstream, so Bring along brings a chat's files there too
pr: 2726
surface: [factory, engine]
invalidates:
  - "An organization's server moved files only within one workstream (factory.org_workstream_file_move {id, files, folder}), so Bring along on an organization left the chat's files behind. The move now takes `to`: the files leave for that workstream's Files, each still itself, and a document takes its page, its versions and its live text. It needs edit on both, and the caller looks after the workstream or made every file (a chat's file counts as made for the person it was made for)."
---

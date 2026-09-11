---
kind: fixed
title: one sentence for every road onto a folder, and a chooser that opens where the person is
pr: 713
surface: [chat, remote, docs]
invalidates:
  - "A bare `/attach` over `--host` answered `choosing a folder is not available over --host yet — the folders here are this machine's, not the ones the conversation is on.` and opened nothing. It opens the context sheet now, on this machine's own disk, exactly as it does locally: `/attach` is a request about a FILE, and a file over a connection travels as bytes into the far session's `attachments/`. #657 had already deleted `/attach takes a path`, so over `--host` somebody who did not know the path had no door at all."
  - "`/attach <a directory>` over `--host` registered THIS machine's path as a scoped reference on the conversation running on the other one, and said `folder · <path>` three lines below the refusal that said it could not be done. It refuses now, with the same one sentence, and registers nothing."
  - "A folder marked on that sheet over `--host` could be confirmed. It says that same sentence, keeps the sheet up and the marks in your hands, and registers nothing; a file marked beside it still reaches the tray."
  - "Three roads onto a folder each decided for themselves what they could say — `openContextPick` asked the connection, `folderConfirm` asked only whether the conversation had a folder door, and `attachFilePath` asked neither. They all ask `app.placeRefusal` now, and where both are true the connection's sentence is the one said."
  - "A conversation opened outside a project opened the chooser on `~/.aforge/v3/projects/<encoded>/<id>/work` — `work/`, `meta.json`, `presence.json`, `transcript.jsonl`, and an action row offering to make aforge's own bookkeeping the thing the conversation was about. That rung is skipped: the sheet opens on the directory this window is working in, which is the ladder the manual has described all along. A folder the conversation already holds still wins ahead of it."
  - "`app.localRoot` was filled in only on a `--host` session. It is this machine's own directory on every session now, because the chooser's ladder needs it on a local one too. `app.pathRoot` still reads it only over a connection, so nothing about where a typed path is anchored changed — `Options.Owned` still completes a typed path against the workspace, deliberately."
  - "The chat corpus said both commands opened the same sheet in the same place, that `/attach <dir>` referred the folder, and that the first folder refusal was `/folder` over `--host` alone. Five pages now say what each road does over a connection, name all three roads that give that sentence, and state that aforge's own state folder is never where the sheet opens."
---

The intent already decided one thing — whether a folder door is required to open the
sheet at all — and #657 wrote that law down in the function that now breaks it. What was
missing is that the CONNECTION is a fact about folders and not about the sheet: files
travel over `--host` and always have, so a browser opened for files is a browser onto
exactly the right disk.

So the refusal moved from the command to the intent, and the three roads that could reach
a folder were made to ask one question. `app.placeRefusal` answers the connection's
sentence before the missing-door one, because where both are true the connection is the
more specific fact and the one a person can act on. The seam itself asks it too, so a road
added later cannot register a local path on a far conversation by forgetting.

No person-facing string changed. `folderRemoteWord` and `folderNoDoorWord` are spelled
exactly as they were; what changed is which roads say them, and where the sheet opens when
nobody has said anything yet.

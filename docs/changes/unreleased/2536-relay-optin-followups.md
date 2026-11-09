---
kind: fixed
title: Storage-off chats stop tools when they move elsewhere, closing sentences arrive, and moved chats keep their words
pr: 2536
surface: [chat, engine, remote, docs]
invalidates:
  - "With hosted storage off, tools in a running shared chat could remain enabled after another computer continued it; a fresh reading now refuses them within about 15 seconds, or at once when the live feed announces the change, and the viewing window admits no new saved turns until explicit continuation."
  - "A closing chat could lose its final saving sentences; they now reach the window once for an individual close, app quit including an earlier slow close, and /new or /resume on a shared connection, with --no-host alike and no repeated sentence already shown live."
  - "An open window could miss the storage answer immediately after pairing; a successful pairing now samples it alongside other finishing work with a separate best-effort budget of about 1.5 seconds, while a timeout or absent answer leaves saved bytes untouched and adds no sentence."
  - "The hosted development build waited on Linux-only /proc/pressure/io on macOS; missing or unreadable pressure files now skip the wait, and the busy-pressure test accepts only timeout status 124 while the documented runtime remains Node 22."
  - "A chat below GIT_CEILING_DIRECTORIES could inherit an enclosing checkout's ignore rules and save an empty chat that another computer could not open; native capture now honors the same Git search boundary as the project's Git commands."
  - "An arriving owned copy could show project: codeaf under --no-host; its recorded origin now keeps the <folder> (copy from another machine) label."
  - "A closing chat could read its released hold and a cancelled authentication could answer 503; closed chats now ignore ownership news, authentication refusals answer 401, and a live request facing a real outage still answers 503."
---
These finish the follow-ups to hosted sync storage being off until an account
is approved. A shared storage-off chat checks who holds it at the tool gate
without starting an uploading writer or renewing a hold: it follows the
directory feed and reuses a reading for at most 15 seconds with it, or two
seconds without it, and queued tools share even a slow failed reading. The
closing receipt's new optional field is ignored by older builds, and an older
engine simply sends none.

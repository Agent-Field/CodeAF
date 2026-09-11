---
kind: fixed
title: enter on a running conversation opens it here, and the window it left steps back
pr: 691
surface: [chat, engine, remote, docs]
invalidates:
  - "Home's `move it here` door wrote a request and waited for a HOLDER WINDOW to answer, whatever was holding the conversation. A conversation held by the workspace's engine host has no holder window, so that wait ran the full ten minutes and ended in `that window has not answered`. Enter on a held row now asks whether the project has an engine answering and, when one does, simply opens the conversation — the engine hands back the session it is already running, mid-turn, in about a tenth of a second. The two-key confirm is left only for a window with no engine behind it."
  - "Two surfaces on one conversation shared it, arbitrated by a keyboard: the newest arrival typed and the older ones watched. A person opening a conversation in the terminal they are standing at now MOVES it. Wire version 13 carries a `moved` frame to every other surface in the room — never to a watcher (`Hello.Watch`) and never on a redial (`Hello.Back`) — and the surface hearing it detaches, says `moved to another window · enter on home brings it back`, and lands on home with that row under the cursor. Nothing pauses: the engine keeps the turn and the tasks. The way back is the same single keystroke."
  - "`session.drainTakeover` left a take-over request on disk until the holder's running turn ended, on the law that a reply is never cut. It answers at the next heartbeat, mid-reply or not. The holder interrupts and closes the way `/new` does, so its work still lands `paused — it resumes` and its partial reply is in the journal the asking window opens."
  - "`aforge chat` refused to attach to an engine host on an older build that was holding work, and told the person to run `aforge engine --stop` — which would have ended the very work they were reaching for. A host speaking this build's wire is now attached to, with one line on the entry notice saying the engine is an older aforge and picks this build up when it goes quiet. A host on a DIFFERENT wire version is still refused."
  - "Home said `open in another window` about every held row. A row the engine holds with no window anywhere now says `open in the engine`."
  - "The manual said chat stops when the terminal closes (`keeping-an-eye.md`). It has not been true since the engine host: the conversation runs in the engine, and `enter` on its row on home brings it back."
  - "The armed take-over sentence promised `it moves when that window's reply ends` and the card's cost clause said `its reply finishes first`. They now say `that window's reply stops there`, which is what the road does."
---

The engine host is what makes work outlive a closed terminal, and it was the one
holder home's move door could never reach. The road that reaches it — a second
connection to the same engine — has existed since the host landed; this is home
taking it, and the window it leaves being told rather than left believing it is
still the one in the conversation.

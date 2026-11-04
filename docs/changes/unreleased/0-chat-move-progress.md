---
kind: fixed
title: moving a chat says how far it has got, stops when nothing comes through, and says how it ended
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "Was true: agents.take had no deadline; its context ended only when the app left, so a move whose relay stream stalled waited forever and held up every move behind it. Is true now: a move stops once a request to the relay has waited 2 minutes with nothing coming through (syncsetup.MoveIdleWithin, ErrMoveStalled); local work between requests does not count."
  - "Was true: a move that ended after the app stopped waiting left nothing on its row but taking:false. Is true now: rows carry move {state, step, done, total} while a move runs or waits, and move_ended {outcome, line, detail} after one that did not move the chat, until the chat is here or another move starts."
  - "Was true: a directory record's size was always 0. Is true now: publishers set it to the bytes of the frames the plan names (0 stays unknown for plans begun before), and a move shows it as its total."
  - "Was true: moves ran one at a time behind a mutex, and nothing could stop one. Is true now: they still run one at a time, behind a slot a queued move can leave, and agents.take_cancel cancels a running or queued move."
---

Moving a chat to this computer said nothing while it ran, had no time limit in
the engine, and, once the app had stopped waiting after two minutes, never said
how it ended.

- **Progress on the row.** While `agents.take` runs, the chat's row carries
  `move`: queued behind another move, or moving, with its step (preparing,
  copying, opening) and the bytes copied of the total the chat's record gives.
  `chats.move` in `hello` says the engine does this.
- **Stops when nothing comes through.** A move whose request to the relay waits
  2 minutes with no bytes arriving stops, and says so: "Stopped: nothing came
  through for 2 minutes. The chat is still on <computer>." Missing objects and an
  unreachable relay get their own sentences; other failures keep the existing
  one, with the error as detail.
- **The outcome stays on the row.** `move_ended` says how a move that did not
  move the chat ended, so the app learns it even after it stopped waiting.
- **Cancel.** `agents.take_cancel {cell}` cancels a running or queued move.
- **The record's size.** Publishers now fill the directory record's `size` with
  the bytes of the frames the plan names, which is what a computer without the
  chat copies.
- **Still sharing.** A row whose holder has turns it has not finished sharing
  carries `sharing {pending, sent, total}`, and `can_take` is false while that
  holder is a known computer. Beats carry the upload's progress
  (`upload_sent`/`upload_total`) only to a relay that answers
  `Codeaf-Upload-Progress: 1`; the Go relay does, the hosted relay does not yet,
  so rows from it say the chat is still being shared without numbers.
- **Asks before a big folder.** Rows carry `folder` (the sealed folder name,
  published only to relays that answer `Codeaf-Chat-Folders: 1`) and `size`,
  so the app can ask "Move this chat here?" before copying more than 100 MB
  (`chatlist.BigMoveBytes`). The terminal's continue card says the same. Chats
  shared in the background now get a size too. Only chats shared after this
  change have a size; the hosted relay does not keep folder names yet, so
  against it the question says "a folder".
- **The terminal** follows a continue on home's line ("copying to this computer:
  412 of 1,433 MB…") and stops after the same 2 minutes with the same reason.

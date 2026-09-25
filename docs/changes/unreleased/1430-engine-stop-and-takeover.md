---
kind: changed
title: engine --status, the older engine gives up the slot, held chats always move, --max-hours ends it
pr: 1430
surface: [engine, remote, chat]
invalidates:
  - "An engine of an older build holding work was attached to and left in place, and `codeaf engine --daemon` found the slot taken and exited without a word. Now an engine from an older build — built earlier, from any file, or too old to answer the version question — is replaced by `codeaf engine --daemon` and by every window that dials in, busy or not, with one line naming the pid, build and binary it replaced. A newer engine is joined, and a tie never replaces. The file-replaced retirement (binary.go) is unchanged and still runs."
  - "Asking which engine holds a workspace meant `ps`. `codeaf engine --status` and `--status-all` ask the socket: pid, binary, build, start, windows attached, conversations open. `--stop` and `--stop-all` now name the process they stopped. There is no `codeaf engine stop` or `status` subcommand; the flags are the spelling."
  - "Enter on a conversation another window held printed `this conversation is open in another window — open codeaf here and press enter on it to move it here` when the holder was an in-process or older window and this window was on the engine road, and nothing was asked. The engine's refusal now reaches home as `session.ErrSessionLocked`, so home asks the holder."
  - "An engine never honoured a move-it-here request (`takeover.json`) for a conversation with no window attached. It now closes that conversation within a second."
  - "A window that did not let go was described only as `that window has not answered yet`. After fifteen seconds the card names it (`held by pid <n> · <tty> · <build>`) and enter offers `Stop that window?`, which sends SIGTERM; a second yes exits it at once."
  - "`takeover.json`'s one field `at` and `presence.json`'s schema-1 fields `pid`, `build`, `state`, `updatedAt` are now a frozen protocol between builds (internal/session/holder.go), pinned by byte tests."
  - "`--max-hours` stopped the work at the wall and left the window open waiting for a person; three `--max-hours 0.15` windows were found alive after 43 hours. The window now leaves two minutes after the wall and exits thirty seconds after that if the ordinary leave has not finished."
---

A two-day-old engine from another binary held a workspace on the owner's
machine, the fresh `codeaf engine --daemon` exited silently, and chats kept
talking to it. The same day, a window on an older build held about ten
conversations and move-it-here did nothing until it was sent SIGTERM by hand.
Both are closed here, and `--status` is the question that used to be `ps`.

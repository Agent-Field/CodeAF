---
kind: fixed
title: a window whose link was repaired takes its task rail back
pr: 764
surface: [chat, remote, engine, docs]
invalidates:
  - "A REDIAL RESTORED THE CONVERSATION AND NOT THE COLUMN BESIDE IT. A turn's events are numbered and replayed, so a repaired link drew its transcript and its status line back and looked entirely well; the standing lanes are neither numbered nor replayed by the protocol — each is a subscription the engine hangs on the CONNECTION — so the task rail, the harness lane and the name went quiet for the rest of the session while the engine went on running that conversation's tasks. The rail then drew `+ /task` alone, which is an empty roster inviting the same work to be started twice (#761). Every standing lane the surface still holds is now asked for again on the new link, and the engine replays what is still true onto it: the whole task roster, a harness card still standing, the conversation's name."
  - "`retakeTitle` was the only lane with this repair and it asked `left != welcome.SessionFile`. The question is `conversationSwitched` now — one spelling for all three lanes and for [Client.reconcile] — so a window that left a conversation with no transcript name (a scripted engine, `aforge engine` on a pipe) is re-watched rather than silently skipped, and a window whose conversation was genuinely replaced under it is still left alone."
  - "Road two of #761 — an older surface on the wire silently losing `task` frames — does not exist and nothing was built for it. A hello on another protocol version is REFUSED at the door, in a sentence, before a session is opened or a lane exists ([server.handshake]); there is no half-attached surface for a rail to go missing on. `TestAnEngineOnAnotherProtocolRefusesRatherThanDroppingFrames` pins it."
---

The window in #761's photograph was not a second window that had been forgotten
about; it was every window, after the host under it was replaced. A host going —
a rebuilt binary, a machine waking up — drops every link at once and each surface
redials with `Hello.Back`, which displaces nobody: both windows stay attached,
one of them is not the driver, and neither one's rail speaks again.

The tmux suite now drives the whole of it (`internal/e2e/secondwindow_e2e_test.go`):
a task started in the first window, a second window opened on the same
conversation through the same host, the host killed under both, and a task
commissioned afterwards — which is a call and not a turn, so its row can only
reach the column down the standing lane.

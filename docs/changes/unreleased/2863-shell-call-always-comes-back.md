---
kind: fixed
title: A shell call always comes back — a wedged command is ended and the turn still replies
pr: 2863
surface: [engine, chat]
invalidates:
  - "Every clock on a bash call (the background-after handoff, the ten-minute timeout, the person's Stop) lived inside the bash tool, so a call wedged anywhere on its way back held the turn until the app was quit (qloop run44: 900 s busy, no model call, no reply). The dispatcher now holds its own clock: a call that has not answered 30 s after the first bound it was going to meet is ended (process group killed), the model is told `Command stopped: it did not finish or answer within N seconds, so it was ended and its result is lost.`, and the turn goes on to a reply. A Stop the call cannot hear ends it after 5 s with `Command aborted: …`."
  - "A foreground command in the desktop app could sit silent for the whole turn. In the app a foreground command that prints nothing for 120 s is now stopped with its output and `Command stopped: it printed nothing for 120 seconds. …`; the bash description says so in the app only. The quiet clock is the same one that already stopped a task's silent search."
  - "A hang left nothing to name it by. When the backstop ends a call it writes every goroutine's stack to logs/stuck/<call>.txt in the conversation's folder."
  - "The app backend read a conversation's title, model, model identity and context use while holding its app-wide lock, so a conversation whose own lock was held could stall every other canvas's requests (the desk's 30 s 'got no answer'). Those reads now happen before the lock; app_lock_order_test.go fails a b.locked closure that calls into a conversation."
---

The root cause of the run44 wedge itself was not reproduced: the same command
shape, a silent `sleep`, a grandchild holding stdout under `setsid`, and a
closed stdin all end normally at the session level. The backstop and the stack
dump are what make the next one end and name itself.

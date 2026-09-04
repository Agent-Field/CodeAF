---
kind: fixed
title: an interrupt from outside reaches Close, so nothing the session owned is orphaned
pr: 605
surface: [chat, engine]
invalidates:
  - "SIGHUP was answered by nothing in this tree — grep found the name nowhere in a Go file. A closed terminal window, a dropped ssh session or a hung-up parent killed the process where it stood, with every defer skipped. It is on internal/leave's set beside SIGINT and SIGTERM now, and takes the same road to Agent.Close."
  - "internal/tui3's forwardSignals is often remembered as the reason the chat door was already covered, and it did answer SIGINT and SIGTERM. What it did not do is keep reading: it took ONE value off its channel and returned while signal.Notify kept that channel registered, so every signal a person sent during the close it had started was delivered to a channel nobody was reading and vanished. A quit that was taking its time could not be hurried by any number of further kills."
  - "runHostOnce — the door behind --host --once and --at --once — submitted its turn under a hardcoded context.Background() with no leaving road at all, while its own comment said it is runChatV3Once with a remote agent and deliberately the same shape. It takes the same leave.On closure now, so the door most likely to be stopped from outside — the ssh session it rode in on going away is a SIGHUP — no longer orphans work that is running on somebody else's machine. TestEveryHeadlessChatDoorTakesTheOneLeavingRoad fails if either headless door loses the road or submits under a context nothing can cancel. The closure also calls remote.Agent.Interrupt, because on this door the cancel alone does not reach the turn: Submit's context bounds the CALL and the work is on another machine. Measured over a real ssh pipe, the cancel by itself left the far turn running sixty-nine more seconds, spending, with nobody attached; Interrupt is a frame and travels."
  - "aforge chat --once had no signal handling of any kind. Its deferred agent.Close was reachable only by the turn ending on its own, which is why #448's real-model line had to use --once returning from its loop to reach Close at all. The road is installed at the top of runChatV3Once now, before the session is opened."
  - "The manual's quitting section said 'Closing the terminal window is not a quit aforge sees; the draft written 300ms after you stopped typing is what survives that.' That sentence is gone: a closed window is a quit aforge sees, and the 300ms debounce is still stated under 'Your unsent draft is kept'."
  - "Nothing anywhere used to force an exit. A second signal is now answered at once, on the shell's own number for it (128 + the signal), and the surface hands the terminal back on a bounded best effort first — so a stop that hangs can no longer trap anybody, at the cost of a terminal that may want `reset`."
  - "The signal set used to be spelled at each door that bothered to catch anything — chatv3_at.go, exec.go, run.go, do.go, cmd/harness-design and enginehost each name their own. internal/leave is the one statement of it for the chat doors, and internal/tui3 has a law test that fails if a signal name or a signal.Notify comes back into that package."
---

Every door a person can leave through has to reach `Agent.Close`, and a signal is
a door. After #448 a close stops every running task and refuses late starts; the
signal road reached none of that, so a `kill -INT` left every node, child agent
and background job the session owned running, and a task mid-call kept spending
until its own process noticed the parent was gone.

The remaining hole is written down rather than fixed: a bare `aforge engine`
serving one pipe still catches nothing. A torn pipe on a non-persistent engine
already reaches `Close` through its own `leave()`, and `aforge engine --daemon`
answers SIGINT and SIGTERM in `internal/enginehost` — but a signal to a pipe
engine whose pipe is still open orphans the same way this change just stopped
the local doors orphaning. Its close semantics turn on `Session.persistent` and
it wants its own change.

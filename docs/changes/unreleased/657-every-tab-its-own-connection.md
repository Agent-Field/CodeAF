---
kind: added
title: every chat tab holds its own engine connection, so opening one stops none of the others
pr: 657
surface: [chat, engine, remote]
invalidates:
  - "Hidden jobs previously appeared idle and Stop work left hidden task nodes and all jobs running. The keeper now retains conversation-local replayed task/job IDs; Stop work cancels that known roster, and hidden jobs publish working/completion state. The engine now closes admission before cancelling the whole conversation, including adaptive runs and concurrent job creation; cancellation news cannot wake another reply."
  - "A tool permission card swallowed tab navigation, and the remote agent could not report hidden attention. Ctrl+W, Ctrl+K and Ctrl+T now leave its question unanswered while navigating, and pushed conversation facts carry needs-you state before the event that wakes its hidden reader."
  - "The remote client lacked Attach and AttachReplay, so hidden tabs lost turn status and reopening could not recover live output. Independent connection-local observations now carry the engine’s atomic transcript/backlog split without submitting again; a broken link ends these observations and reopening refreshes them."
  - "Chats previously counted only running task nodes when describing held work, so a streaming reply without tasks said nothing new. It now reads the existing turn watcher and says working; completed-turn news remains available until the conversation is reopened."
  - "Home could call a conversation held by this window open in another window; it now uses the keeper identity and says open here. The close-tab card clipped its right-hand answers at narrow widths; it now shortens complete labels while retaining visible click targets and a k shortcut for keep running."
  - "Opening or switching to a second conversation over an engine-backed door — the ordinary socket onto this machine's engine, `--host`, `--at` — ENDED the first one. The surface said so, out loud, with `closed · <name> — a connection holds one conversation at a time`. It does not any more: each conversation dials a connection of its own and every one of them keeps running while you are somewhere else. The wording still exists for a door that has no way to dial again, and no shipped door is in that state."
  - "`aforge chat --no-host` was the ONLY way to hold two conversations open at once, and the manual sent people to it for that. It is no longer special in this respect; it remains the door whose conversations die with the terminal, because there is nothing else to run them."
  - "`tui3.Options.SharedAgent` was true for every engine-backed door, and `Options.Fresh`/`Options.Resume` were what those doors filled in. They now fill in `Options.Start` and `Options.Open` — the whole-`Conversation` seams — and set `SharedAgent` only when they cannot dial a second connection. Nothing in the tree does that today."
  - "`remote.Hello` had no way to say \"mint me a conversation and join none of the existing ones\": `Session` empty meant the workspace's latest-or-new, so two connections arriving at once could land on one conversation. `Hello.New` is that word, and `enginehost.Host` answers it by booting a session under a free key. A redial never repeats it, so a dropped link returns to the conversation it minted rather than minting another."
  - "`enginehost.Host.open` looked up a live session only by its hello key. It now also finds one by transcript path, so reopening a minted conversation by its file attaches to the agent already running it instead of booting a second one onto the same journal."
  - "`tui3`'s link seam — the held question, the driver, the followed turn — was surface-wide and bound to whichever connection the window booted on. It rides `Conversation` now and is rebound as tabs change, because a held consent card belongs to the conversation that raised it and must never be replayed into another."
  - "`ctrl+w` and the `×` on a tab closed it outright. A tab whose conversation is writing a reply, running tasks or holding a question now raises a card in the guard slot — `keep running`, `stop work`, `cancel`, cursor on `keep running`, `esc` cancels, `s` stops. An idle tab still closes on the first press with no card. `keep running` takes the tab off the row and leaves the conversation in the keeper, still on the switcher and on Home; reopening it shows everything it did while it was out of sight."
---

The defect was reproducible in one sentence: open three tabs, ask the first one a
question, switch, and watch it be closed underneath you. The surface was not lying about
it — `SharedAgent` and the `closed · …` line were the honest report of a real
constraint — but the constraint was the wrong one to have.

It lived in exactly one place. The engine host had been multi-session since the wire
version 2 split: `Session` is the conversation and `server` is one connection, and
several connections may already point at one session. What was single was the SURFACE's
connection. `Options.Fresh` and `Options.Resume` over an engine door both handed back the
same `*remote.Agent`, and `session/new` and `session/open` were answered by `Session.swap`,
which interrupts and closes whatever was open. One mutable pointer cannot be two
conversations.

So the fix is one connection per conversation, and not conversation-id multiplexing.
Multiplexing would have meant a conversation field on every frame, a demultiplexer in
front of the event stream, and per-conversation cancellation of a shared reader — a
rewrite of a 62KB protocol file — bought for one file descriptor, on a road where ssh is
already `ControlMaster=auto` multiplexed and the socket is a unix socket on this machine.
`cmd/aforge/chatv3_beside.go` holds the fleet: a dial per conversation, an `engineConn`
that closes once, and a `besideAgent` whose `Close` and `Detach` retire its own connection
and nobody else's. That is the whole lifecycle ownership, in one file, for all three doors.

The boot connection is the exception, and deliberately: World, Ledger, Memory, Search,
Archive, RecentSessions and the task record are dispatched against a session on the wire
but are facts about the MACHINE, and they keep answering after a conversation ends. It is
never retired by a conversation closing and is given back once, by the door, when the
window does.

The tab-close card is the other half, and it is a UX change rather than a confirmation
dialog. Closing a tab has never ended work — the `×` closes a view — but a person could
not see that, and could not ask for the other thing without leaving the tab row. Three
answers name three different acts, the cursor opens on the one that loses nothing, and the
card does not dismiss itself when the work finishes underneath it, so an answer arriving a
moment before your press cannot turn `stop work` into a press that lands on nothing.

`stop work` uses a conversation-local engine operation that blocks new work before
cancelling the reply, queued/running tasks, adaptive runs and jobs. Cancellation
news stays available without waking another reply. Reopening preserves the stopped
conversation; only a fresh user submission resumes admission after cancellation
settles. Older engines that cannot perform this operation return an explicit error.

Sibling local-engine tabs retain the originating launch settings. Previously,
reopening a tab created by Ctrl+T could fail the interactive-launch compatibility
check and fall back onto the journal held by its own engine, leaving pending
input unreachable behind a takeover wait. The ordinary reconnect rejoins it now.

Title subscriptions replay the current name without a redundant newer attention
snapshot invalidating the replay. Pending-question events still publish attention
before waking hidden conversation readers.

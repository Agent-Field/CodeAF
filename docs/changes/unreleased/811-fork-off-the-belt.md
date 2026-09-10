---
kind: removed
title: '`fork` is off the belt: a reply no longer copies itself into hands'
pr: 811
surface: [chat, engine, docs]
invalidates:
  - "`fork` was a tool on every belt but a hand's own: mid-work, the running mind copied itself into two to four HANDS, each opening on the caller's whole transcript, each holding a declared slice of the caller's working copy. It is GONE — not refusing, absent, so the model does not have the verb. `Agent.forkTools`, `forkHands`, `forkSeed`, `newHandAgent`, `forkBelt`, `runHand`, `handLeash`, `forkOut`, `handReport`, `forkNote`, `forkCharge` and the rest of the verb are deleted with their tests, and the belt bullet in `beltfacts.go` (`Config.mayFork`) with them. What replaces it is the quick task (docs/design/quick-task/DESIGN.md): work that runs where the caller works and whose last message is its result — but it is a task node with a row, a room and a stop, not a copy of the caller's mind."
  - "A hand was a JOB. `jobKindHand`, the published `JobKindHand`, `jobRegistry.startHand`, its outstanding-hand count (`hands`, `handHome`, `handsOutstanding`) and `stopHands` are deleted, so `jobs list` has no hand row, the tool-result footer names no hand, an interrupt stops no hand, and `awaitableKind` is background commands alone. `Agent.childrenOutstanding` no longer asks about hands: a node's landing waits on its task family and on nothing else."
  - "A hand's landed writes were stashed on the session and drained into whichever turn next reached a step boundary (#635). That machinery — `Agent.handWrites`, `stashHandWrites`, `drainHandWrites` — is deleted with the verb that fed it. The write seam counts the running turn's own batch again, which is all there is to count. `docs/changes/unreleased/635-fork-writes-count-at-the-seam.md` is deleted for the same reason: it is a release note about a capability that no longer ships."
  - "`Config.inHand` and `Config.handLeash` are gone from the agent's config. Nothing set them once the verb went, and `interactive_budget.go`, `principal_wire.go` and `hooks.go` each read one of them; the round-budget citizen is no longer registered on any agent."
  - "`actioncategory.go` listed `fork` under `ActionCoordinate`. It does not, and the conditional-verbs test no longer names it."
  - "`checkpoint.go` carried a special sentence for a fork burst — one call, one batch, four agents inside it. A round is one batch and not one call for the plain reason, with no verb named."
  - "`internal/manual/chat/tasks.md` carried five sections about hands — *Hands — several parts of one answer worked at the same time*, *Do hands get around the write limit*, *A hand is a stream, not a wait*, *How a hand's slice of files is spelled*, *What hands cannot do* — and they are deleted. So are the hand sentences in `how-tasks-run.md` (`fork` is on a task's belt), `what-i-can-do.md` (a hand on the job footer, in `jobs list`, and its kill line), `models-and-cost.md` (hands on the status line and in `/cost`'s `tasks` row), `what-i-remember.md` and `compacting-over-and-over.md`. The probes that reached those sections are removed; *can you work on several parts of my answer at once* is now vocabulary on *How many tasks run at once*, which is the section that answers it."
  - "What is KEPT, unchanged, is the declared write scope: `normalizeScopePath`, `normalizeWriteScope`, `workspaceShown`, `scopeAsGuarded` and `forkScopesCollide` still live in `internal/session/fork.go`, which is now that file's whole subject. `writeGuard` (orchestrate.go) reads them exactly as before, and the quick task's `files` claim is their second caller."
---

A capability that cannot be reached is absent, not present and refusing
(CLAUDE.md). `fork` came off the belt on the owner's ruling in
docs/design/quick-task/DESIGN.md, so it comes out of the code, out of the job
registry, out of the config, and out of the manual in the same change — because
a tool named in the corpus satisfies the gate perfectly while lying about what
the program can do.

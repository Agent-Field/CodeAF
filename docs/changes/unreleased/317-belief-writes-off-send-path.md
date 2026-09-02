---
kind: fixed
title: the belief write leaves the send path, and no lock in internal/lane waits
pr: 317
surface: [chat, engine]
invalidates:
  - "`ledger.Note`, `NoteOutcome`, `Prime` and `NoteThinking` compacted the belief file inline — on the first observation of every process and on every 512th after — through `ledger.keep` → `compact` → `store.hold` → `filelock.Lock(gate, exclusive, blocking)`, all of it under the ledger mutex the chooser reads on every encode. `flock(2)` takes no deadline and Go cannot interrupt it, so one process holding `v3/lanes.json.lock` stopped every model call in this one; a measured run sent nothing at all for 29m49s. The doors now append one journal line and signal a size-one channel, and the compaction happens on a writer goroutine and nowhere else."
  - "Every advisory lock in `internal/lane` was taken blocking. None is now: `store.locked` and `journal.drain` ask five times over about 300 ms and then answer a new `errLockBusy`, and `journal.add` and `journal.read` — which really are on the send path — ask once and proceed regardless. A lock that is merely BUSY is told apart from a filesystem with no advisory locking, which still writes unlocked exactly as before. `internal/filelock` is unchanged."
  - "Nothing ran the belief writer, because there was none. `lane.Persist(ctx)` is the process's one writer and `lane.Flush()` writes down what it has not reached; `cmd/aforge`'s `execute` starts the first beside `calllog.Open` and runs the second at the same one exit, so `do`, a subharness and the chat surface are all covered. `internal/lane` still starts no goroutine of its own, the way `lane.Beat` does not."
  - "A process that wrote no state file had lost what it learned. It has not: every observation reaches `lanes.log` before the writer is ever signalled and `ledger.rebuild` makes memory a pure function of the state file and the journal, so a compaction that never happens costs a longer replay and never a belief. What this does change is WHEN `lanes.json` appears — a test or an embedder that reads the file back after an observation must call `Flush` first, which `internal/lane/store_test.go`'s two multi-process tests now do."
  - "A deferred belief write was not a thing that could happen, so nothing counted one. `ledger.Deferred()` counts a compaction that could not take the lock and one line goes to the call log naming the reason and the running count, because a write that was quietly dropped is the original freeze with the symptom removed rather than the cause."
  - "`hedgeRace.run`'s select had two cases, `decided` and `results`, and no `ctx.Done()`. An arm folds its answer into the belief BEFORE it posts its result (`client.go`'s `noteVelocity`), so an arm stuck in that write left a loop nothing could end — which is why Escape sat in `stopping` for minutes. The loop now leaves on its context, withdraws the offer, notes the two budget denominators because a cancelled request was still made, and deliberately does not settle: the losing arms were never allowed to finish, and folding their waits in would teach the ledger that a lane it cut off is slow."
---

The evidence was a chat run that issued no model call for 29m49s while the
surface said `waiting for deepseek-v4-flash · nothing has come back yet · slow`.
The tell was in the files: `v3/lanes.json` froze at the moment of the failure
while `v3/lanes/<model>.json` — written before any lock is taken — kept
advancing for another half hour. The network was fine; a lock was not.

PR #253 removed the per-`Note` save, which was the large half, and said in its
own comment on `keep` that what remained was this. It is the whole of it now:
the exclusive lock is taken on one goroutine that nobody's first token is
behind, and every lock in the package is asked for rather than waited on.

The writer is the process's rather than the package's because `internal/lane`
runs no goroutine of its own by design — `Beat` says so at its own door — and it
is `cmd/aforge`'s rather than a session's because `do` and a subharness measure
lanes without ever building one.

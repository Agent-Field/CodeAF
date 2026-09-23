---
kind: fixed
title: the chat's run digest leads with the live run, in the side list's words, on every message
pr: 1393
surface: [chat, engine, docs]
invalidates:
  - "The digest in front of the person's sentence took the first eight rows in the order the store read them, oldest ended run first, so a conversation with eight rows of history saw eight `cancelled` rows and none of the live run. It now leads with the live run, and every row says the side list's word (queued, running, done, stopped, incomplete, your call) through `PlanTaskRow.StateWord`, which the `tasks` listing of a run's rows now says too. The store's words (pending, ready, claimed, failed, cancelled, paused) are no longer what the model reads about a row."
  - "Only a plain sentence carried the digest. A message with pictures and a draft marked standing now open on it as well, and what a message's words are to the rest of the engine (the recall, the owed answer, the ask a `forward` carries) is the person's sentence, never the digest or the standing instruction in front of it."
  - "`tasks` with `say` or `forward` on a row of a run answered `No task \"2\" in this project`. `say` is now written onto the row as a note and its answer says so; `forward` refuses and points at `note`."
  - "The `note` field's description and its receipt said `revise_assignment` changes what a task was asked for. The chat never carries that verb; both now say nothing the conversation holds changes a run task's brief, and name `stop` and a fresh hand-off."
  - "Saving the `provider` row wrote `lane.talk.borrow`, which no list of consumed keys held, so every later launch warned that it was ignored. It is consumed now, and `TestProfileKeyLedgerLaw` drives every settings row's writer, so a key a writer puts down that nothing registers fails the law."
  - "`Assisted-by` named the model the conversation was launched on, even after `/model`. A switch now re-renders the page at the clock stamp it already had, so the line names the live model, and switching back is byte for byte the page the earlier model has cached."
  - "#1209 (rolled into v0.4.0 with `invalidates: []`) dropped two things from the answer section without saying so. It cut the ban's examples to `Want me to…` alone — no `Sure`, `Great question`, `You're right` or `Say the word and I'll…` — while keeping both bans; that was the price of its byte budget, and it stands. It also dropped `never hand back half-solved work` from what done means, which was not intended: the page says `never a compiling scaffold, a narrowed test or half-solved work` again, paid for inside the same section. A test that used `Say the word and I'll` as a sentence only the chat's page carries went on passing without testing anything; it now checks its sentinels are still on the page."
  - "The manual said a note reaches its worker between steps, after the command it is running. It is handed over as a steer the moment a step ends: a reply being written is cut and asked again, and a foreground command of three seconds or more is moved to the background."
---

The digest's mapping and the side list's are one table by law:
`TestTheRowsWordIsTheRailsWord` reads internal/tui3's `planStateWord` out of
the tree and fails on any word the two say differently, until the side list
calls `StateWord` itself.

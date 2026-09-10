---
kind: fixed
title: the auto-settle floor survives a restart — a landing aforge was deciding comes back yours
pr: 751
surface: [chat, engine, docs]
invalidates:
  - "Who holds a landed-but-unsettled task's decision (`TaskNode.decider`, `TaskAskOwnerModel` vs the person) used to live ONLY on the live node, and `TaskNode.decider`'s own comment said `IT IS NOT ON THE CHECKPOINT`. It is on the checkpoint now: `taskRecord.Decider` (`\"decider\"` in `<session>/tasks.json`), written by `recordLocked` and read by `restoreNode`, with the emptiness law — a record that says nothing says the person."
  - "A restored graph used to come back the person's because the zero value read that way, which was the right answer with no act behind it. It now comes back the person's because the FLOOR RUNS ON LOAD: `TaskGraph.handBackOnLoad`, called out of `rehydrate`, hands every model-held node to the person as the checkpoint is read — before the frontier turns, before anything is drawn, and before `recoverTasks` rewrites the file — and `recoverTasks` publishes one ordinary task update for each landing still waiting. An engine that died mid-turn used to drop that hand-back entirely, because `handBackUnsettled` only ever runs at the end of a turn that got to end."
  - "`readsTheDecisionLocked`'s guard — is this the turn the decision was handed into — is NOT asked on the load road. There are no turns on it: every agent that was holding anything died with the process."
  - "No fixture could seed `aforge is deciding`, which is why the task-states acceptance could not provoke shape 5. It can now: a `tasks.json` node carrying `\"decider\": \"model\"` is the record a process killed under `task.settle = auto` leaves behind. `internal/e2e`'s `TestTaskStatesE2E` gains `the_auto_settle_floor_hands_a_restart_back`, which pays for no model and asserts the attached surface draws the chips and NOT the `aforge is deciding` row; the live subtest keeps its skip path."
  - "The PROJECT INDEX deliberately does not carry the decider, and this is a ruling rather than an omission: `internal/session/task_index.go`'s file is what work came to, appended once and never rewritten, while who holds a question lasts at most one turn — a row saying `aforge is deciding` about a conversation that closed hours ago is a claim nothing can correct. It is `TaskIndexEntry.Activity`'s rule about a present that ends seconds after it is recorded."
  - "The questions wave's derived landing `Question` used to carry no `Policy` at all. It now reads one off `TaskAsk.Owner` through `landingPolicy` — model is `PolicyDecide`, person is `PolicyAsk` — so there is ONE holder across both vocabularies. There is still no second timer: `PolicyRecommendThenAuto` and `Question.Deadline` belong to the `ask` tool's timed assumptions (`tools_ask.go`), and a landing question carries no deadline because the floor is the end of a turn rather than a clock."
---

A person who closed aforge over a card that said `aforge is deciding` had given the
question to a turn that died with the process — nothing was going to wake a model to
finish that thought, and nothing was going to hand it back. The record now says who was
holding it, which is what gives the floor something to fire on when the conversation
opens again.

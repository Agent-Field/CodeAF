---
kind: fixed
title: an accept whose merge was refused says so, and the landing it left behind can be answered again
pr: 776
surface: [chat, engine]
invalidates:
  - "A landed `your call` used to be drawn and answered by internal/tui3's tasksettle.go — its own two rows, its own five letters, its own receipts, and three copies of the routing for the conversation, the room and the roster. It is a session.Question now: the engine builds it over TaskAsk (question.go's landingQuestion), publishes it the moment the node lands and again every time the node moves (task_landing_question.go), and the question block draws it on every page. tasksettle.go keeps the card lookup and the redraw and nothing else."
  - "The tmux suite's words for that question have moved out of internal/tui3 and into internal/session, and internal/e2e/tuiwords_test.go names them there. Two of them are gone rather than moved: `[d] let aforge decide this one` and `[t] take it back` are off the row — docs/design/questions/DESIGN.md rules that LandingDecideKey `d`, LandingAgainKey `r` and LandingTakeBackKey `u` are reachable through the door but not on the row."
  - "Anything that read `you looked at this yourself and took it as done` as the accept receipt is stale. The receipt names who spent the verb: `you took this as done` for the person's own press, `aforge took this as done` where the model spent it under `task.settle = auto`. The door used is carried as a fact and is never guessed from the policy."
  - "A landing refused because the person's own UNTRACKED copies of the task's files sit in the folder used to report as `conflicts with your branch`, and `[a]` on it spent a merge round, which merges branches and cannot see an untracked file at all — so it refused again in the same words. It is a road of its own now (refusedByYourFiles, TaskFacts.GroundHeld), reads `your folder already has files the task wrote: a.md, b/`, and its `[a]` carries the person's copies aside, lands the branch, and puts them back — keeping theirs beside the task's as `<name>.yours` where both wrote the same path. Nothing of theirs is ever deleted."
  - "Agent.ResolveUnverified is no longer the only door onto a landing: the model's `tasks <id> resolve …` goes through resolveUnverifiedBy with the model as the decider, and the tool's reply is read back off the node afterwards rather than assumed. A refused accept replies that the task is still your call and that nothing merged and nothing failed."
---

A divided task landed as the owner's call under `task.settle = auto`. The model
accepted it, the merge was refused by the owner's own uncommitted copies of the
very files the task had written, and every surface said otherwise: the tool
replied that the branch had come home, the model said the task was done, the
children wore a receipt for a look nobody took, and the one screen that could
have been answered had no answer on it at all.

The cause underneath all of it was that nothing published the landing's
question. It existed as an object and only an attaching window ever saw one, so
a node that re-landed asking something different went on drawing the first
landing's frozen shape — with the surface's own de-dup guard throwing the new
notice away before it could have redrawn anyway.

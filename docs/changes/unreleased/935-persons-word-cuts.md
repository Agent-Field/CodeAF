---
kind: fixed
title: a model you pick while work is running takes at the next request, not the next turn
pr: 935
surface: [chat, engine, remote]
invalidates:
  - "A model chosen while the agent is working landed at the NEXT TURN — `Agent.SetModel`'s own contract said so and `runTurn` latched the model once per turn. It lands at the next REQUEST now, within `lane.SpokenWithin`: a request that has produced nothing the person could use is cut and asked again on the model they named, and a request whose answer is already arriving finishes and is followed by one on the new model. `runTurn` re-reads the person's word at one site, the top of the request ladder."
  - "`internal/tui3`'s room answered a task model pick with `its next turn takes it`. That sentence is deleted. The room says `switching now` when the request in flight was let go of and `the next request takes it` when an answer was already arriving, and no person-facing sentence about a model pick says the word `turn` — a task step IS one turn and can run for twenty minutes."
  - "`Agent.RetargetTask` returned only an error. It returns `(session.ModelLanding, error)` — `ModelLandsNow` or `ModelLandsNextRequest` — and the landing travels over the `--host` wire. An engine too old to send one is read as `ModelLandsNextRequest`, which is the true half of the sentence when we cannot know."
  - "Two causes cut a generation (`errRecallCut`, `errMarkCut`) beside the person's steer. There are three of the person's own now: `errPersonCut` is a model they named reaching a request that had produced nothing. A cut for it is journaled and says NOTHING to the person — nothing failed and they had read nothing."
  - "A failing step walked its own fallback ladder past a model the person had just named, and a step whose completer offered no chain ended the turn on `there is nowhere else to try` with their choice sitting unasked. The person's word is the head of every chain now: the move takes it where the move is really made, so the sentence a person reads names the model their reply actually went to, and a word standing is itself somewhere left to go."
  - "`Agent.Steer` cancelled `a.generation` itself. `Agent.cutGenerationLocked` is the only place in `internal/session` that cancels a generation, and `internal/session/generation_law_test.go` fails the build on a second one."
  - "`Agent.SetReasoning` said a level latches for the turn `exactly as the model does`. The two rules are deliberately different now — a level lands at the next Submit, a model at the next request — because turning the thinking up is setting a level for the next thing you ask and changing models is redirecting work you are watching go the wrong way."
  - "The five manual claims that a model picked inside a task's room takes effect on `the task's next turn` (tasks.md, models-and-cost.md twice, how-tasks-run.md, screen.md, task-controls.md) are replaced by the next REQUEST and the two landings. models-and-cost.md has a new section for somebody who pressed a model mid-reply."
  - "#925 landed the half of this that could be said a day earlier and its words are superseded here, not contradicted. The room's `the next turn takes it; a rescue goes to it first` is gone: the rescue no longer needs a clause of its own, because the pick rides the next request whether that request is a rescue, a retry or the step's own next step. And #925's two conversation sentences — `a pick in a conversation you are sitting in front of still lands on your next message` and `a model you pick with /model does not redirect a rescue that is already happening` — are both false now and are replaced in models-and-cost.md. #925's `TaskNode.picked` is untouched and still owns where a node's NEXT WORKER is built; this owns the request already out."
---

The owner's evidence, 2026-09-11 14:40:10: a task step on `waiting · rate limited
· 13m 37s`, a model picked in the room, `continue` typed, and at 14:41:00 the step
still talking to the model they had moved off. The pick had landed on the agent —
nothing made the request in flight let go of it. "Unproductive" is defined once, as
a state and never a duration: the request has put nothing in front of the person.

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
  - "`Agent.Steer` cancelled `a.generation` itself. `Agent.cutGenerationLocked` is the only place in `internal/session` that cancels a generation, and `internal/session/generation_law_test.go` fails the build on a second one."
  - "The five manual claims that a model picked inside a task's room takes effect on `the task's next turn` (tasks.md, models-and-cost.md twice, how-tasks-run.md, screen.md, task-controls.md) are replaced by the next REQUEST and the two landings. models-and-cost.md has a new section for somebody who pressed a model mid-reply."
---

The owner's evidence, 2026-09-11 14:40:10: a task step on `waiting · rate limited
· 13m 37s`, a model picked in the room, `continue` typed, and at 14:41:00 the step
still talking to the model they had moved off. The pick had landed on the agent —
nothing made the request in flight let go of it. "Unproductive" is defined once, as
a state and never a duration: the request has put nothing in front of the person.

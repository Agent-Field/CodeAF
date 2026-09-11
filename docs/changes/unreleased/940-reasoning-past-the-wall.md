---
kind: fixed
title: a planning model that thinks past its wall is asked for its answer instead of losing it all
pr: 940
surface: [engine, resident]
invalidates:
  - "A planning call with no effort configured sent no reasoning knob at all: `config.Config.Context` sends `WithConfiguredReasoningEffort(ctx, c.Reasoning)` and `DefaultReasoning` is `provider.EffortNone`, so a model like GLM 5.3 thought at its published default `max` with nothing on the wire bounding it. Every completion through a walled structuring slot (`pool.DefaultCallWall`, the resident's chat and plan slots and `aforge do`) now carries its wall to the effort ladder (`provider.WithThinkingWall`), and a pass nobody else sized is sent a thinking budget of `thinkingShare(level) × the lane belief's rate for the asked machine × the wall` — 47,880 tokens at 210 tok/s under four minutes. A word somebody chose still travels as their word, a model that thinks only when asked is still not asked, a rung's own budget yields only to a smaller wall, and a machine the ledger holds no belief about derives no budget. The call log records such a request's effort as `max within 4m0s`, not by its count."
  - "A completion that outlived its call wall was simply lost: `walled.CompleteWithMessages` returned `nil, ErrCallWall`, and nothing could have done better through the provider, because a raced call's `hedgeRace.abandon` returns the caller's deadline and nothing else. The walled decorator now keeps what the completion streamed and, when the wall is reached with thought or answer on the wire, asks ONCE for the answer that thought reached — the caller's messages, the thought as the assistant turn, an answer ask, thinking switched off, the request's own shape — under a wall of its own. Only if that fails too does the caller get `ErrCallWall`. A completion that wrote nothing is still not asked again."
  - "It was believed (in this wave's own brief) that the stream-wall work of #786 already salvaged a cut answer on the chat road. It never did: #786 re-arms the stream wall for a stream that keeps pace, and a stream-wall cut on the chat road still throws the reply away whole. The only cut that keeps anything is the structuring slots' call wall, since this change."
  - "`resident.commandWall` was ten minutes, argued as roughly twice the worst honest case. Issue #927's two strikes were that rail on honest commands. It is now `spliceRounds × pool.LongestCall` — five sequential structuring rounds, each at most a completion and its answer ask — which is forty minutes on a four-minute call wall."
  - "`ErrCallWall` said `the model stopped answering`, the stall note said `the model stopped answering — trying again`, and the receipt said `I couldn't get this planned — the model stopped answering twice`. Silence never reaches that wall (the stream guard cuts it first, with its own sentence), so all three now say `the model thought past its time`."
---

Issue #927 lost two four-minute grounding passes to the call wall and two whole commands to
the ten-minute rail on a request nobody would call dishonest, while every first token had
arrived in under a second. The model was never told how long it had, a cut kept nothing it
had thought, and the receipt blamed a silence that did not happen.

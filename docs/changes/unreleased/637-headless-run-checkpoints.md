---
kind: fixed
title: a run left going checkpoints on every door, not only where somebody is watching
pr: 637
surface: [chat, engine]
invalidates:
  - "`Agent.checkpoints` — the ONE door into the whole checkpoint road, and the gate the write seam, the turn-wall share, the three marks, the ceiling, the stopped-turn re-open and the looping handover all enter through — refused every session with `Config.AskConsent` false. That flag says A PERSON IS WATCHING THE EVENTS and every headless door deliberately leaves it false, so a run left going on its own without a screen (`aforge chat --once \"…\" --yolo --max-hours 6`, which appoints the very same `Steward` the conversation door appoints) never checkpointed at all: no mark was read, no seam fired, no share of the wall was counted, and #534's reading of every ending by the session's own goal owner sat behind a gate that had already said no. The gate reads the PRINCIPAL now. A node is still refused first and for its own reason; after that a session is refused only when there is nobody to put the decision to — no person watching AND no goal owner — which is the reading `Agent.settlePolicy` already took with its arms the other way round. A headless run left going with a ceiling now crosses the same marks at the same rounds as a conversation, moves its work on the same roads, and has every ending put to the same owner before it seals."
  - "The old law was written down as `A SCREENLESS SESSION NEVER CHECKPOINTS`, in the gate's own comment and in `TestTheTurnsThatMustNeverBeCheckpointedAreNot`'s row name. Half of it was right and is unchanged: a headless `--once` with no ceiling, and a `--yolo` run that named none, both hold a `Person` and are still refused — they pay for no mark reading, start no task and run their turn to its end. What was wrong was the other half, and the law is now spelled for what it actually protects: a screenless session with NOBODY LEFT IN CHARGE never checkpoints."
  - "`internal/manual/chat/how-tasks-run.md` named a bare `headless --once` among the doors that keep the old road for a landing nobody could check. That was already wrong before this change — the road there is chosen by the conversation's goal owner (`TaskNode.unattendedRun` reads the principal and nothing about the door), so a `--once` carrying a ceiling has taken the unattended road since #534. It now says a headless `--once` WITH NO CEILING. The unattended section of `starting-aforge.md` carries the `--once` form beside the three conversation ones and says the posture is the same without a screen, and `docs/HEADLESS.md` lists `--max-hours` and `--max-cost` on that door."
---

The unattended posture is one posture on every door. Whatever `--yolo` with a
ceiling decides for a conversation that hands work over, stops, or lands a check
nobody could run, it decides identically for a run with no screen — the door
changes how the decision is SHOWN, never whether it is MADE. It was not one
posture: everything #534 built for an unattended ending was reachable only
through a gate that asked whether a person was watching, and the door most
likely to be left running overnight is exactly the one that answers no.

The fix is the gate and nothing else. The road behind it already knew what to do
the moment it was reached, and `route_judge.go` — which starts work INSTEAD of a
reply at the front of a turn rather than reading the ending of one — keeps its
own screen gate deliberately.

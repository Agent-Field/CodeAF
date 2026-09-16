---
kind: added
title: The approvals chip — what a conversation runs without asking is a control on the seam
pr: 0
surface: [chat]
invalidates:
  - "YOLO was a launch-only posture. If you remember `--yolo` as the only way to open the gate, a conversation now has its own posture on the tool gate, moved from inside it: `alt+y` walks `asks → guardian → YOLO → asks`, a press on the `◇` cell on the legend is the same step, and `/approvals <word>` (alias `/yolo`) sets `ask`, `guardian`, `yolo`, `deny` or `auto` outright. The posture is live — the next tool call is decided under it — and sticky in the session's `meta.json`, so it survives `/resume`. `codeaf resume --yolo` outranks the saved word for that launch."
  - "The `YOLO` badge on the status row was the one place the gate's posture was drawn, only when the gate was open, and it was a door onto `/permissions`. In an open conversation it is gone from the row: the legend above the message box carries `◇ asks` / `◇ guardian` / `◇ YOLO` / `◇ refuses` at EVERY posture, after the thinking rung, with the open gate in the warning hue. The row's `YOLO` is drawn only on the frames whose legend has no chip — the welcome box and a task's page — and it is a reading there, not a door. The phone status sheet's `approvals` row says the posture at every posture."
  - "The `guardian` settings row was a fourth thing to set beside the mode. On the wheel it is one stop: `guardian` is prompt with the small model standing in, `ask` is prompt with it stood down whatever the row says. `internal/session.Config.Guardian` is still the launch's answer; the conversation's posture overrides it live ([session.Agent.SetApprovalPosture])."
  - "`cmd/codeaf`'s `v3Policy(workspace, profileDir, yolo bool)` is a wrapper now; `v3PolicyMode(workspace, profileDir, mode)` is the one function every posture builds through, and `v3ApprovalGate` is the door handed to the engine as `session.Config.ApprovalGate`. `refreshV3Policy` and `applyV3Approvals` rebuild through the engine's own `RebuildApprovalGate` where the agent has it, so a rule banked inside a conversation walked to YOLO lands on a YOLO gate."
  - "Over `--host` the posture crosses the wire: `internal/remote` carries `MethodResolvedApproval` and `MethodSetApproval`, `Welcome.Approval` says whether the far engine has the door, and `session.Facts.Approval` rides the photograph so the seam reads it from memory. An engine without the door leaves the cell a reading of `Welcome.ApprovalMode`, and the chord, the press and the command say the far machine's rules decide."
---

The gate had an install scope (the settings rows) and a launch scope (`--yolo`)
and nothing a person standing in a conversation could move — the same hole the
thinking rung had before the thinking chip, answered the same way. The
conversation's posture is one word for a pair (the blanket answer and whether
the guardian stands in), built by the same `v3PolicyMode` the flag builds
through, so a person's exceptions, shell rules and both floors are identical at
every stop.

The wheel deliberately has three stops. `deny` one press past `YOLO` would be a
wheel that breaks a session by accident, so it is reached by name only; `auto`
— follow the settings rows again — is by name for the same reason.

The chord is `alt+y`. `ctrl+y` is the copy-a-path key on home and in `/files`,
and one chord means one verb on this surface; `shift+tab` walks backwards
through every question card's fields and arrives as a plain `tab` on some
terminals.

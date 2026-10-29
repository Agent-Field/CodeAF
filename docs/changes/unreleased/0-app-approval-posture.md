---
kind: added
title: codeaf app sets a chat's own approval posture and a new agent's effort
pr: 0
surface: [engine]
invalidates:
  - "Was true: a chat's own approval posture (ask, guardian, allow, auto) could be set only from the terminal's chip and /approvals. Is true now: agents.set_approval_posture sets it from the app, through the same conversation door and meta.json word; deny is refused there, because Block is Settings' answer and a chat follows it with auto. A factory stage refuses it as it refuses a model change."
  - "Was true: the Agent view said nothing about the gate. Is true now: a held agent carries approval {posture, source}: the resolved posture (ask, guardian, allow or deny) and whether it follows Settings (settings) or was set in this chat (chat)."
  - "Was true: agents.create took no effort or posture, so a new agent always opened on the scopes above it. Is true now: agents.create takes effort (the agents.set_effort words) and approval_posture (the agents.set_approval_posture words), checked the same way, and sets them on the conversation before its first turn."
  - "Was true: settings said asks_before only, which cannot tell Ask from Guardian. Is true now: settings also says approval_posture, what asks_before and the guardian row amount to in the posture ladder; it is read only."
---

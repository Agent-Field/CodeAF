---
kind: fixed
title: Lazy app opening preserves message refusal and live crew holds
pr: 0
surface: [engine]
invalidates:
  - An invalid client id could reach conversation opening before refusal; all desktop message doors now validate it first.
  - Live factory stages and established crew holds could wait on an unchanged disk stamp; they now remain due independently of the stamp, as do kept services.
---

Idle crew members still stay unopened. Once the crew holds a member, an engine
closing does not end that hold: a paused stage can be held again, and the
factory's resumed stage is lent again. Message acceptance still opens under the
agent gate before consulting the conversation's capabilities and history.
Client ids retain their existing correlation-only meaning.

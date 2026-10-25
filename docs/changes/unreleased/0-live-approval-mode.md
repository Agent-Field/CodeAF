---
kind: fixed
title: Permission mode changes reach running sessions on their next tool call
pr: 0
surface: [chat, engine, remote]
invalidates:
  - "Running sessions could keep the approval policy built at launch after Settings saved a new mode. Sessions now follow current settings at their next tool decision."
---

The app saved `tools.approvalMode`, but the session's approval decision read
only its launch policy or a policy explicitly pushed by a surface. Changes in
Settings therefore needed a relaunch unless that surface rebuilt the gate.

Running conversations now refresh through the shared approval gate before a
tool decision: changing allow to prompt starts asking, and changing prompt to
allow stops asking. Conversation postures, launch overrides, project rules and
dangerous-command floors retain their precedence. A decision already waiting
for approval keeps its existing question.

Concurrent refreshes previously checked only the conversation posture, so an
older settings read could reopen a newer prompt gate. Refresh versions now
reject stale publication and select the decision snapshot under the same lock.
Profile read and JSON errors now reach the gate builder instead of becoming
absent settings, so a failed refresh retains the previous policy.
Factory stages reuse their fixed allow policy instead of compiling it for every
decision. Clarification conversations follow the parent's live gate; ordinary
task workers retain their fixed allow policy.

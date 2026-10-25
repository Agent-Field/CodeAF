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
older settings read could reopen a newer prompt gate. Versioned publication
still let a tool that read prompt fall back to an old allow policy when a
replacement refresh had started but not finished. A dedicated approval mutex now
serializes each settings build through publication and decision snapshot selection;
conversation posture changes and explicit policy pushes use the same mutex.
The general agent mutex is released during builds, so stalled settings reads
no longer block Interrupt, StopWork, consent resolution or Close.
Unchanged settings still reuse the content-keyed policy cache.
Profile read and JSON errors now reach the gate builder instead of becoming
absent settings, so a failed refresh retains the previous policy.
Factory stages reuse their fixed allow policy instead of compiling it for every
decision. Clarification conversations follow the parent's live gate; ordinary
task workers retain their fixed allow policy.

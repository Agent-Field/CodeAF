---
kind: added
title: codeaf app says when an agent has something new for the person
pr: 0
surface: [engine]
invalidates:
  - "Was true: an agent on the app wire did not say whether it had said anything since the person last looked, and the engine kept no seen mark. Is true now: every Agent carries `unread`, true when it answered, finished a run with a report, or put a question to the person after the last `agents.mark_seen {id}`; tool steps and thinking alone never set it. The mark is kept in the app's state file, survives a restart, never opens or warms the agent and never reopens a closed or archived one. `agent.changed` is sent when unread turns on and when a mark clears it, and when output said in another process (the terminal, another window) moves it. A question counts only while it is open: answering it, from any door, clears needs-you without leaving the agent unread, and a restart reads it the same. Agents with no mark start read, so an upgrade shows no dots; the engine lands before the desktop that marks agents seen, because `unread_from` is kept from the first `agents.list`."
  - "Was true: a conversation's meta.json said when the person last spoke but not when the conversation last answered. Is true now: `lastReplyAt` is stamped beside the spend when a turn seals on an answer."
---

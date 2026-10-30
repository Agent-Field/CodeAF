---
kind: fixed
title: codeaf app never opens an agent the person closed unless they open it
pr: 2041
surface: [engine]
invalidates:
  - "Was true: the crew keeper held open every agent with work to recover (a task record not yet noted, a process to recover), including one the person had just closed or archived. Every open takes an agent out of Closed, and an agent let go a moment ago counts as recently active, so such an agent was opened again within a poll of its close, at every launch too, and its row came back. Is true now: the keeper, a read of a crew member's tasks or asks, and the roads that carry on work a quit or a dead holder left (keepOpen, keptLive) leave an agent marked closed or archived unopened. The mark is read under the agent's gate, where a close writes it. Only the person's own open, a send, or Reopen takes it out."
---

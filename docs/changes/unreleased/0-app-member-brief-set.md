---
kind: added
title: a member's brief changes in place, with its history kept
pr: 0
surface: [engine]
invalidates:
  - "A member's brief could not be changed: the app reposted new instructions as a message in the member's conversation. members.brief.set {crew, agent, text} now changes it in place: a `brief` line in the crew log, read by the member at its next step, waking nobody and restarting nothing."
  - "A receipt or delivery in a member's chat had no way to open its own crew log line: crews.log.at {agent, post, crew_name?, around?} answers the crew and the line with its neighbours, found by the agent's membership and the line's id (never words or time)."
  - "members.brief answered only the start line. It now answers the brief in force and `revisions` (who, when, before and after, the log entry), kept on the membership in the teams file (teams.ReviseBrief, Member.Briefs), so the history survives a restart."
---

`teams.KindBrief` is a new traffic kind; `teams.ReviseBrief` is the one writer
(log line and revision under one lock), refusing a stale `base`, a closed team,
and a brief longer than the 12000 characters a member is delivered. Only the
oldest kept revision stores its `before`; the rest read it from the revision
before. A membership with no revisions was never changed here; nothing is
inferred for older records. `teams.ReadAround` reads a line with its neighbours
by seeking to its id.

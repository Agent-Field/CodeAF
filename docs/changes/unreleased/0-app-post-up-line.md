---
kind: fixed
title: A post to the crew above names the line it became, so its receipt opens it
pr: 0
surface: [engine]
invalidates:
  - "A member of a sub-crew with no manager posting to the manager above was answered without the line's number or the crew's id, so its handoff had no `post` and nothing could open it. The answer now says the crew above by name and id and the line as `#N`; the handoff carries `crew` and `post`."
  - "`crews.log.at` read only crews the agent is a member of, by name. With `crew` it also reads the crew above for a member of a crew under it, and only that member's own line there."
---

`teamPostUp` keeps the id `AppendTrafficID` gives the line; `ReadTeamPost` reads
the new sentence and the older one (which opens nothing).

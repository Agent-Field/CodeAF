---
kind: added
title: an automation records the crew it was made in, and codeaf app reads a crew's orders by it
pr: 0
surface: [engine]
invalidates:
  - "Was true: no automation recorded where it was made beyond its conversation, and a crew could not list what was made in it. Is true now: an automation created in a turn that crews.message opened records Origin.Team with that crew's exact id, validated at creation; an ordinary accepted message records known none; every other context (replay, task note, automation run, tool-opened, a row saved before the field) stays UNKNOWN and is never read as none. An older app build that sends a crew message as a plain Submit to a new engine is indistinguishable from an ordinary message and records known none; only builds that send the stamp record the crew."
  - "Was true: Automation.Crew, the crew an order belongs to, was the only crew field. Is true now: it is unchanged and unvalidated at creation; Origin.Team is a separate fact, never written by change or update, never moved by a manager or membership change, and never inferred from membership, the current manager or the workspace."
  - "Was true: the crew stamp lived only in the app layer. Is true now: Submission.CreationTeam and remote SubmitArgs.creationTeam carry it on the plain, attached, queued, flushed, retried and edited crew paths and on a new member's first brief (members.start, which has no closed-crew guard, so a stamped turn on a closed crew refuses its automations); an engine built before it ignores it and records no team (unknown). The stamp is a live-turn fact: a turn replayed after a restart carries none and its automations are unknown."
  - "Was true: orders.list was the only orders read. Is true now: an engine naming orders.made_in serves {crew, limit?, cursor?} as a bounded page of the orders whose recorded creation team is that crew, with unknown, the store-wide count of orders no team was recorded for; orders.list keeps its shape and filter."
---

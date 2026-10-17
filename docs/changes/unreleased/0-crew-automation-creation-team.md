---
kind: added
title: an automation records the crew it was made in, and codeaf app reads a crew's orders by it
pr: 0
surface: [engine]
invalidates:
  - "Was true: no automation recorded where it was made beyond its conversation, and a crew could not list what was made in it. Is true now: an automation records Origin.Team from the creation context the door that accepted its turn's opening message stated — the crew's exact id for crews.message and a new member's first brief (members.start), explicit none for the person's own send in codeaf app. A door that states nothing (plain Submit, the terminal surface, an older app or client, words the app writes itself, a row saved before the field) leaves it UNKNOWN; absence is never read as none."
  - "Was true: a message carried nothing about the crew it was sent for. Is true now: session.Submission.Creation (*CreationContext, nil = unknown, {} = none, {team} = exact) and remote SubmitArgs.creation carry it; the engine checks at acceptance that the team exists and holds the receiving conversation and refuses a forged or unrelated team as a mark that does not fit. A closed crew is accepted exactly as its door accepts it. The context is journaled on the person's line, so a turn asked again after a takeover keeps its crew; the id is never re-checked or moved by a later close, manager or membership change."
  - "Was true: Automation.Crew, the crew an order belongs to, was the only crew field. Is true now: it is unchanged and unvalidated at creation; Origin.Team is a separate fact, never written by change or update, and never inferred from membership, the current manager or the workspace."
  - "Was true: a host that keeps no message marks refused nothing about crews. Is true now: still true — a crew message to such a host is sent as its words, and the team is unknown there."
  - "Was true: orders.list was the only orders read. Is true now: an engine naming orders.made_in serves {crew, limit?, cursor?} as a page of the orders whose recorded creation team is that crew, read in one statement over at most 500 stored rows, with unknown, the count of untagged or unreadable orders in the same stretch; a surface sums unknown over the walk. orders.list keeps its shape and filter."
---

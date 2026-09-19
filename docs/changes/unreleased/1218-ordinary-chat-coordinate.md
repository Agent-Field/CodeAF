---
kind: added
title: an ordinary chat can coordinate others
pr: 1218
surface: [chat, engine]
invalidates:
  - "cmd/codeaf opened collections.db for folders and left Config.Collab, Options.Collab, and the inbound router unset, so coordinate and TUI k/c were absent even after the Wave 3 packages merged."
  - "wsapi's collabStore used a private collabRow, so *workspace.Store could not satisfy it and CoordinateSelected stayed absent on the production path."
  - "Participant and delivery origin CHECKs refused agent, so a representative envelope could not be stored."
  - "Wave 3 coordination was described as a future planner/critic bus. Production is ordinary-chat coordinate through one router on collections.db: direct, fan-out, or optional joint discussion. There is no execute grant."
  - "tuiCollab.Activity listed only pending deliveries to the coordinator, so outbound request/sent and recorded replies never painted."
  - "Invite recorded a roster row and did not run a bounded per-participant invocation or Contribute, so planner/critic labels had no production invocation door."
---

Nil collections.db still leaves the verbs off the belt. Discussion/invocation
rows stay process-local until a later schema; deliveries are durable.
Activity paints request/reply/sent from ListChatTraffic. Invite consults
RoleCollabConsult then Contribute. Pause refuses new deliver/invite.


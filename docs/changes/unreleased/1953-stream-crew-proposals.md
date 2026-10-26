---
kind: changed
title: Crew proposals stream their shape and helpers beside the chat
pr: 1953
surface: [engine, remote, chat]
invalidates:
  - "Crew proposals previously waited for one complete JSON object. They now emit ephemeral crew.proposal.delta events for readable NDJSON objects before opening the final ask."
---

The conversation's own model proposes the smallest useful crew, normally one to
three helpers. Separate teams are suggested only for separate streams of work
that each need multiple people. The stream sends shape, name, optional teams,
and members in reporting order. Malformed lines are dropped. Each delta carries
agent, a sequence starting at one, and the parsed line object.

A stopped stream with at least one helper still opens the usual crew_proposal
ask, with the helpers received and a stopped note. No usable helpers keeps the
quiet notice. Proposing runs no turn and changes neither the journal's messages
nor the next turn's context. The existing nonstreaming side ask remains available.

Accepting records reporting handles on members of one flat crew. Nested crews
are not created. Engines without streaming support retain the final-card path.

Streaming proposals explicitly disable reasoning for this side ask alone, with the
provider's lowest supported effort used when reasoning is mandatory. The previous
optional low-effort hint could be omitted when the catalog did not recognize the
model, leaving it to deliberate at its own default. Chat and ordinary side-ask
reasoning settings are unchanged. A flat shape with zero explicit team groups is
accepted and emitted immediately, rather than dropped before the name line.

Proposal acceptance now returns only after the original manager and every helper
are seated. Start traffic is claimed under the same lock that appends it, so the
watch and another window cannot create the same helper again. Approved helper
handles take priority over automatically derived manager or existing-member
handles. Invalid or duplicate edited helpers are refused before creation. A
membership handle failure is returned rather than silently accepting a renamed
helper. This fixes the partial setup and conflicting names reported in Q03.

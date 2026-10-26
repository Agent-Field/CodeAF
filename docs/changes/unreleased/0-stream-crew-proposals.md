---
kind: changed
title: Crew proposals stream their shape and helpers beside the chat
surface: [engine, remote, app, chat]
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

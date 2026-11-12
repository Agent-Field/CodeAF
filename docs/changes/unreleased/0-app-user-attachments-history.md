---
kind: fixed
title: a person's own words and attachments are kept on their message's line and survive a reopen
pr: 0
surface: [engine, remote]
invalidates:
  - "The app's attachment chips and the person's own words lived only in the process that sent them (appTurnMark), so after a restart a message read back as the path-expanded words the model was sent, with no attachments. session.Agent.SubmitMessage now records the person's words and bounded attachment descriptors (id, kind, name, path, url, and bytes/lines as pointers where nil is unmeasured and zero is a measured zero) on the message's own journal line, and DisplayEntry carries them as one nil-able Person pointer (Authored, Attachments), so an entry stays 288 bytes."
  - "appTurnMark carried a message's text and attachments for listings. It now carries only the turn and client id; agents.entries reads words and attachments from the record, and a line without the mark has no attachments, whatever paths its words mention."
  - "remote SubmitArgs carried words alone. It now also carries said and attachments on Submit; an engine without SubmitMessage runs the words and keeps no mark."
---

The mark rides the line's existing `presentation` field (audience `person`), so a
compaction pass, a steer and the record cache read it with the line and nothing
keeps a second history. The model is sent exactly the words it was sent before.
A compaction pass, a rewind and an open now cut the in-memory presentation index
back to the messages the session still holds, so a mark never keeps a folded or
cut message's paste alive; the marks stay on the journal's lines.

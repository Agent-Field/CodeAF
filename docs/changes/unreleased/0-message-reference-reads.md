---
kind: added
title: a person's message keeps the reads of references its words were written from
pr: 0
surface: [engine, remote]
invalidates:
  - "A message the app wrote by expanding a collection kept only the expanded words: what the read found at send time (how many references, how many gone, unaskable, unread or cut) was lost, and asking the collection again later answers a different question. session.Submission now carries Reads []MessageRead (Source and Items as MessageReference{Kind, ID, Name, Owner, Path, URL}; Count, Missing, Unavailable, Unread, More, Complete as the door observed them), recorded on the message's own line in the existing presentation mark and read back as DisplayEntry.Person.Reads. DisplayEntry is still 288 bytes."
  - "session.Agent.Steer and FollowUp took words only, and a steer that fell through became a plain message. SteerMessage and FollowUpMessage take a Submission, and a fallen-through steer carries its mark into the message it becomes. The unexported errSubmissionMark is now session.ErrMessageMark."
  - "agents.send and crews.message took text, client_id and staged attachment ids only, and the entry showed the words the app typed. They now also take said (the person's own words, never replaced by the expanded text) and reads; an engine that is not an appwire.MessageSender answers such a send unknown_method, and hello names messages.reads when it is. Entry.reads carries the reads on the live user entry and on every history page. agents.steer, agents.queue and queue.edit still take words only."
  - "remote SubmitArgs carried said and attachments. It now also carries reads; an engine without SubmitMessage still runs the words alone and keeps no mark."
---

A read is a fact the door observed, never one the engine works out: nothing is
looked up again, nothing is derived from the words, and the model is sent
exactly the text it was sent before. Bounds refuse the whole message before
anything is queued or journaled (16 reads, 8 items and 8 warnings per read,
4096-byte fields, 512-byte warnings, counts 0 to 1,000,000, 128 KiB of read
strings, no `complete` while anything was unavailable, unread or cut); nothing
is trimmed to fit.

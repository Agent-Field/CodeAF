---
kind: added
title: a person's message keeps the reads of references its words were written from
pr: 0
surface: [engine, remote]
invalidates:
  - "A message the app wrote by expanding a collection kept only the expanded words: what the read found at send time (how many references, how many gone, unaskable, unread or cut) was lost, and asking the collection again later answers a different question. session.Submission now carries Reads []MessageRead (Source and Items as MessageReference{Kind, ID, Name, Owner, Path, URL}; Count, Missing, Unavailable, Unread, More, Complete as the door observed them), recorded on the message's own line in the existing presentation mark and read back as DisplayEntry.Person.Reads. DisplayEntry is still 288 bytes."
  - "session.Agent.Steer and FollowUp took words only, and a steer that fell through became a plain message. SteerMessage and FollowUpMessage take a Submission, and a fallen-through steer carries its mark into the message it becomes. The unexported errSubmissionMark is now session.ErrMessageMark."
  - "agents.send and crews.message took text, client_id and staged attachment ids only, and the entry showed the words the app typed. They now also take said (the person's own words, never replaced by the expanded text) and reads; an engine that is not an appwire.MessageSender answers such a send unknown_method, and hello names messages.reads when it is. Entry.reads carries the reads on the live user entry and on every history page."
  - "agents.steer, agents.queue and queue.edit took words only, and silently dropped an attachments field. They now also take said and reads, through the optional appwire.MessageSteerer and appwire.MessageQueuer (hello agents.steer.reads and agents.queue.reads, each said only for its own door), and refuse attachments as bad_params. Queued gains reads, and Queued.text is the person's words, never the expanded text the queue will send. A plain queue.edit drops the old said and reads; a marked one is checked whole before the queue changes; an edit after the send answers sent. A conversation whose hosted engine cannot keep the mark answers unknown_method before anything is steered, queued or sent."
  - "remote SubmitArgs carried said and attachments. It now also carries reads, on Steer and FollowUp as well as Submit. Welcome.SteerMarks and Welcome.FollowUpMarks say a hosted agent keeps a correction's and a follow-up's mark, separately from MessageMarks; the client refuses a marked SteerMessage or FollowUpMessage with no call made when the welcome did not say so, and the server refuses one before the turn or the queue when the agent has no door."
---

A read is a fact the door observed, never one the engine works out: nothing is
looked up again, nothing is derived from the words, and the model is sent
exactly the text it was sent before. Bounds refuse the whole message before
anything is queued or journaled (16 reads, 8 items and 8 warnings per read,
4096-byte fields, 512-byte warnings, counts 0 to 1,000,000, 128 KiB of read
strings, no `complete` while anything was unavailable, unread or cut); nothing
is trimmed to fit.

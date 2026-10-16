---
kind: added
title: an ask says whether anything waits on it, a held ask's clock waits until it is shown, and the app can count what is new since a place
pr: 0
surface: [app, engine]
invalidates:
  - "An app ask said nothing about whether work waited on it, and an empty holding_up was the only hint. Ask.blocking is now true for a permission and for the model's own question that parks its turn or names tasks, false only for the model's own question whose turn carried on and names no task, and absent (unknown, read as waiting) for every other kind."
  - "A choice's clock started when the question was raised, so a surface that held a question until the person paused let the suggestion be taken unseen. asks.defer stops the clock of a blocking:false ask until asks.shown starts it for its whole length (remote Question.Clock); the engine starts a deferred clock by itself after five minutes, and a person's hold is never undone."
  - "Counting what came after the place a person last read meant paging the whole conversation. agents.since {id, after} counts the turns, entries and distinct changed files after one entry from the record's cached shapes. Turns are told by the record's own marks (a person's message, a wake, a crew delivery, a completed or stopped reply) and are absent where those marks cannot tell turns apart; files is absent past 400 calls."
  - "A message the queue sent when a turn ended looked the same as one the person sent at once. A user Entry the engine sent from the queue now carries queued:true, kept on the message's own line in the record (session Submission.Queued), so history and a reopened conversation say so too; older lines and immediate sends carry nothing."
  - "A task page could count what was new since a place only from the pages it had loaded. tasks.since {id, after} counts the task's own record the same way agents.since counts an agent's, from one read of the room journal tasks.entries reads."
---
